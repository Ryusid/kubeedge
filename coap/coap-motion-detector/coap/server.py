import asyncio
import logging
import threading
from datetime import datetime
import cv2
from aiocoap import resource, Context, Message, Code, numbers

log = logging.getLogger("Coap-server")

# CoAP resources

class MotionResource(resource.ObservableResource):
    def __init__(self):
        super().__init__()
        self.motion = b"false"  # "true" or "false"

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

class ImageResource(resource.Resource):
    def __init__(self):
        super().__init__()
        self.jpeg = b""

    async def render_get(self, request):
        # latest ROI jpeg
        return Message(payload=self.jpeg, content_format=numbers.media_types_rev['image/jpeg'])

    def set_jpeg(self, buf: bytes):
        self.jpeg = buf

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

# helpers to bridge detector -> resources

def on_rise_factory(image_res, last_res, motion_res, jpeg_quality=85):
    def on_rise(crop_bgr):
        log.info("Rising edge: motion TRUE")
        if crop_bgr is not None and crop_bgr.size > 0:
            ok, buf = cv2.imencode(".jpg", crop_bgr, [int(cv2.IMWRITE_JPEG_QUALITY), jpeg_quality])
            if ok:
                image_res.set_jpeg(buf.tobytes())
        ts = datetime.now().strftime("%Y-%m-%d_%H-%M-%S")
        last_res.set_now(ts)
        motion_res.set(b"true")
    return on_rise

def on_fall_factory(motion_res):
    def on_fall():
        log.info("Falling edge: motion FALSE")
        motion_res.set(b"false")
    return on_fall

async def start_server(bind_ip="192.168.8.222", bind_port=5683, motion_detector_loop=None):
    motion_res = MotionResource()
    last_res = LastDetectionResource()
    image_res = ImageResource()
    class_res = ClassResource()

    site = resource.Site()
    site.add_resource(['motion'], motion_res)
    site.add_resource(['lastdetection'], last_res)
    site.add_resource(['image'], image_res)
    site.add_resource(['class'], class_res)

    await Context.create_server_context(site, bind=(bind_ip, bind_port))
    log.info("CoAP server listening on coap://%s:%s", bind_ip, bind_port)

    # ---- run detector in a background thread (not in the asyncio loop!) ----
    if motion_detector_loop:
        t = threading.Thread(
            target=motion_detector_loop,
            args=(on_rise_factory(image_res, last_res, motion_res),
                  on_fall_factory(motion_res)),
            name="MotionDetectorThread",
            daemon=True
        )
        t.start()
        log.info("Motion detector thread started")

    # keep asyncio loop alive
    await asyncio.get_running_loop().create_future()
