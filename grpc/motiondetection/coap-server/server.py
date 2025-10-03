import asyncio
import logging
import threading
import time
import uuid
from datetime import datetime
import cv2
from aiocoap import resource, Context, Message, Code, numbers

import grpc
import infer_pb2, infer_pb2_grpc

log = logging.getLogger("Coap-server")

# ------------------- CoAP resources -------------------

class MotionResource(resource.ObservableResource):
    def __init__(self):
        super().__init__()
        self.motion = b"false"

    async def render_get(self, request):
        return Message(payload=self.motion, content_format=numbers.media_types_rev['text/plain'])

    def set(self, val: bytes):
        if self.motion != val:
            self.motion = val
            self.updated_state()

class LastDetectionResource(resource.ObservableResource):
    def __init__(self):
        super().__init__()
        self.ts = b""

    async def render_get(self, request):
        return Message(payload=self.ts, content_format=numbers.media_types_rev['text/plain'])

    def set_now(self, s: str):
        new = s.encode()
        if new != self.ts:
            self.ts = new
            self.updated_state()

class ClassResource(resource.ObservableResource):
    def __init__(self):
        super().__init__()
        self.label = b""

    async def render_get(self, request):
        return Message(payload=self.label, content_format=numbers.media_types_rev['text/plain'])

    async def render_put(self, request):
        new = (request.payload or b"").strip()
        if new != self.label:
            self.label = new
            self.updated_state()
        return Message(code=Code.CHANGED, payload=b"ok")

    # --- NEW: allow server-side updates (from gRPC) ---
    def set_label(self, new: bytes):
        new = (new or b"").strip()
        if new != self.label:
            self.label = new
            self.updated_state()

# ------------------- gRPC helpers -------------------

async def make_grpc_stub(addr: str):
    """Create a gRPC aio channel + stub and wait until it's ready."""
    opts = [
        ('grpc.max_send_message_length', 50*1024*1024),
        ('grpc.max_receive_message_length', 50*1024*1024),
    ]
    ch = grpc.aio.insecure_channel(addr, options=opts)
    await ch.channel_ready()
    stub = infer_pb2_grpc.InferenceStub(ch)
    return stub, ch

async def classify_and_update(jpeg_bytes: bytes, class_res: ClassResource, stub: infer_pb2_grpc.InferenceStub):
    """Call gRPC classifier and write result into /class."""
    try:
        req = infer_pb2.Frame(data=jpeg_bytes, id=str(uuid.uuid4()), meta="image/jpeg")
        t0 = time.perf_counter()
        resp = await stub.Infer(req)
        rtt_ms = (time.perf_counter() - t0) * 1000.0
        label = (resp.label or "").encode()
        class_res.set_label(label)
        log.info("[gRPC] label=%s  infer_ms=%.1f  grpc_rtt=%.1f", resp.label, resp.infer_ms, rtt_ms)
    except Exception as e:
        log.warning("[gRPC] classify failed: %s", e)

# -------------- bridge detector -> resources -------------

def on_rise_factory(last_res, motion_res, class_res, loop, grpc_stub, jpeg_quality=85):
    """
    Called from the detector thread.
    Saves JPEG (+ /image), updates /lastdetection and /motion,
    then schedules an async gRPC classify → /class update.
    """
    def on_rise(crop_bgr):
        log.info("Rising edge: motion TRUE")
        jpeg = None
        if crop_bgr is not None and crop_bgr.size > 0:
            ok, buf = cv2.imencode(".jpg", crop_bgr, [int(cv2.IMWRITE_JPEG_QUALITY), jpeg_quality])
            if ok:
                jpeg = buf.tobytes()

        ts = datetime.now().strftime("%Y-%m-%d_%H-%M-%S")
        last_res.set_now(ts)
        motion_res.set(b"true")

        # schedule gRPC classify (best-effort, even if jpeg is None)
        if jpeg:
            asyncio.run_coroutine_threadsafe(
                classify_and_update(jpeg, class_res, grpc_stub),
                loop
            )
        else:
            # still clear any old label if you want:
            class_res.set_label(b"")  # optional
    return on_rise

def on_fall_factory(motion_res):
    def on_fall():
        log.info("Falling edge: motion FALSE")
        motion_res.set(b"false")
    return on_fall

# ------------------- server entry -------------------

async def start_server(bind_ip="192.168.8.222", bind_port=5683,
                       motion_detector_loop=None,
                       grpc_addr: str | None = None):
    """
    Start CoAP telemetry server and (optionally) launch the motion detector thread.
    When rising edge fires, image is classified via gRPC and label is written to /class.
    """
    motion_res = MotionResource()
    last_res   = LastDetectionResource()
    class_res  = ClassResource()

    site = resource.Site()
    site.add_resource(['motion'],        motion_res)
    site.add_resource(['lastdetection'], last_res)
    site.add_resource(['class'],         class_res)

    await Context.create_server_context(site, bind=(bind_ip, bind_port))
    log.info("CoAP server listening on coap://%s:%s", bind_ip, bind_port)

    # --- gRPC client (aio) ---
    grpc_addr = grpc_addr or "127.0.0.1:50051"  # override via run.py/env
    try:
        stub, grpc_channel = await make_grpc_stub(grpc_addr)
        log.info("gRPC classifier connected: %s", grpc_addr)
    except Exception as e:
        log.error("gRPC connect failed (%s). Labels won't be set until it connects.", e)
        # You could retry in a task; for simplicity we raise:
        raise

    # ---- run detector in a background thread (not in the asyncio loop!) ----
    if motion_detector_loop:
        loop = asyncio.get_running_loop()
        t = threading.Thread(
            target=motion_detector_loop,
            args=(on_rise_factory(last_res, motion_res, class_res, loop, stub),
                  on_fall_factory(motion_res)),
            name="MotionDetectorThread",
            daemon=True
        )
        t.start()
        log.info("Motion detector thread started")

    # keep asyncio loop alive
    await asyncio.get_running_loop().create_future()
