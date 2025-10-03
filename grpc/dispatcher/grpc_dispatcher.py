#!/usr/bin/env python3
import os, time, asyncio, statistics, collections
import grpc, psutil
import infer_pb2, infer_pb2_grpc

def env(k,d): return os.environ.get(k,d)

BIND         = env("DISPATCH_BIND", "0.0.0.0:50052")
LOCAL_ADDR   = env("LOCAL_ADDR", "127.0.0.1:50051")
CLOUD_ADDR   = env("CLOUD_ADDR", "192.168.8.108:50051")   # <- set yours

# Policy knobs
CPU_HIGH       = float(env("CPU_HIGH", "75"))    # %
CPU_LOW        = float(env("CPU_LOW",  "40"))    # %
P95_TARGET_MS  = float(env("P95_TARGET_MS", "150"))
MAX_LOCAL_CONC = int(env("MAX_LOCAL_CONC", "4"))  # local concurrent requests
Q_HIGH         = int(env("Q_HIGH", "32"))        # backpressure fallback

PING_INTERVAL  = float(env("PING_INTERVAL_S", "2"))
RPC_TIMEOUT    = float(env("RPC_TIMEOUT_S", "3"))
WIN            = int(env("LAT_WIN", "50"))       # sliding window for local latencies

# ----------------------------------------------------------------

class EMA:
    def __init__(self, alpha=0.3):
        self.alpha = alpha
        self.v = None
    def update(self, x):
        self.v = x if self.v is None else (self.alpha*x + (1-self.alpha)*self.v)
        return self.v
    def value(self):
        return self.v if self.v is not None else 0.0

class LatWin:
    def __init__(self, n=50):
        self.buf = collections.deque(maxlen=n)
    def add(self, x): self.buf.append(x)
    def p95(self):
        if not self.buf: return 0.0
        s = sorted(self.buf)
        idx = int(0.95*(len(s)-1))
        return s[idx]
    def avg(self): return statistics.mean(self.buf) if self.buf else 0.0
    def __len__(self): return len(self.buf)

async def make_stub(addr):
    opts = [
        ('grpc.max_send_message_length', 50*1024*1024),
        ('grpc.max_receive_message_length', 50*1024*1024),
    ]
    ch = grpc.aio.insecure_channel(addr, options=opts)
    await ch.channel_ready()
    return infer_pb2_grpc.InferenceStub(ch), ch

class Dispatcher(infer_pb2_grpc.InferenceServicer):
    def __init__(self, local_stub, cloud_stub):
        self.local = local_stub
        self.cloud = cloud_stub
        self.local_sem = asyncio.Semaphore(MAX_LOCAL_CONC)

        # telemetry
        self.cpu_ema = EMA(alpha=0.3)
        self.ping_local = EMA(alpha=0.3)
        self.ping_cloud = EMA(alpha=0.3)
        self.local_lat = LatWin(WIN)

        self._q = asyncio.Queue()  # only for an estimate of inbound pressure

    async def background(self):
        # CPU sampler + pingers
        async def sample_cpu():
            while True:
                self.cpu_ema.update(psutil.cpu_percent(interval=None))
                await asyncio.sleep(1.0)
        async def ping(stub, ema):
            while True:
                try:
                    t0 = time.perf_counter()
                    # "ping": zero bytes + meta="ping" → no artificial delay on worker
                    await asyncio.wait_for(stub.Infer(infer_pb2.Frame(data=b"", meta="ping")), timeout=RPC_TIMEOUT)
                    dt = (time.perf_counter()-t0)*1000.0
                    ema.update(dt)
                except Exception:
                    ema.update(1e9)  # treat as very high
                await asyncio.sleep(PING_INTERVAL)

        tasks = [
            asyncio.create_task(sample_cpu()),
            asyncio.create_task(ping(self.local, self.ping_local)),
            asyncio.create_task(ping(self.cloud, self.ping_cloud)),
        ]
        await asyncio.gather(*tasks)

    # Policy: decide local vs cloud
    def _choose(self):
        cpu = self.cpu_ema.value()
        p95 = self.local_lat.p95()
        pl  = self.ping_local.value()
        pc  = self.ping_cloud.value()
        qsz = self._q.qsize()
        # Hard reasons to go cloud
        if cpu >= CPU_HIGH: return "cloud", f"cpu={cpu:.0f}"
        if p95 > P95_TARGET_MS: return "cloud", f"p95={p95:.0f}"
        if qsz > Q_HIGH: return "cloud", f"q={qsz}"
        # Prefer local when cool
        if cpu <= CPU_LOW and p95 <= P95_TARGET_MS: return "local", f"cool cpu={cpu:.0f} p95={p95:.0f}"
        # Else compare ping with some margin
        if pc + 5 < pl:  # cloud ping clearly better
            return "cloud", f"pc={pc:.0f}<pl={pl:.0f}"
        return "local", f"default cpu={cpu:.0f} p95={p95:.0f} pl={pl:.0f} pc={pc:.0f}"

    async def Infer(self, request, context):
        await self._q.put(1)  # just to reflect inbound pressure
        try:
            target, reason = self._choose()
            if target == "local":
                # concurrency-guarded local call
                async with self.local_sem:
                    t0 = time.perf_counter()
                    try:
                        resp = await asyncio.wait_for(self.local.Infer(request), timeout=RPC_TIMEOUT)
                    finally:
                        self.local_lat.add((time.perf_counter()-t0)*1000.0)
                # Optionally tag response id/label suffix with route (omit if you don’t want it)
                return resp
            else:
                resp = await asyncio.wait_for(self.cloud.Infer(request), timeout=RPC_TIMEOUT)
                return resp
        except Exception as e:
            # If cloud/local fails, try the other as a fallback
            try:
                alt = self.cloud if target=="local" else self.local
                resp = await asyncio.wait_for(alt.Infer(request), timeout=RPC_TIMEOUT)
                return resp
            except Exception:
                await context.abort(grpc.StatusCode.UNAVAILABLE, f"both targets failed: {e}")
        finally:
            self._q.get_nowait()
            self._q.task_done()

async def serve():
    local_stub,  _lch = await make_stub(LOCAL_ADDR)
    cloud_stub,  _cch = await make_stub(CLOUD_ADDR)
    disp = Dispatcher(local_stub, cloud_stub)

    server = grpc.aio.server()
    infer_pb2_grpc.add_InferenceServicer_to_server(disp, server)
    server.add_insecure_port(BIND)
    print(f"[dispatcher] on {BIND}  (local={LOCAL_ADDR} cloud={CLOUD_ADDR})")

    # kick off background samplers
    asyncio.create_task(disp.background())

    await server.start()
    await server.wait_for_termination()

if __name__ == "__main__":
    import psutil  # ensure import ok at runtime
    asyncio.run(serve())
