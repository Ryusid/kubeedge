#!/usr/bin/env python3
import os, time, asyncio, random
import grpc
import infer_pb2, infer_pb2_grpc
import logging

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")

def env(k,d): return os.environ.get(k,d)

log=logging.getLogger(env("GRPC_LOGGER", "GRPC"))

BIND = env("GRPC_BIND", "0.0.0.0:50051")
INFER_DELAY_MS = int(env("INFER_DELAY_MS", "10"))
CLASS_NAMES = [s for s in env("CLASS_NAMES","person,car,dog,cat,other").split(",") if s]

class InferenceServicer(infer_pb2_grpc.InferenceServicer):
    async def Infer(self, request, context):
        # Fast path "ping": meta="ping" or empty payload → no artificial delay
        is_ping = (request.meta.lower()=="ping") or (len(request.data)==0)
        t0 = time.perf_counter()
        if not is_ping:
            await asyncio.sleep(INFER_DELAY_MS/1000.0)  # simulate/replace with real model
        label = "pong" if is_ping else random.choice(CLASS_NAMES)
        infer_ms = (time.perf_counter() - t0) * 1000.0
        return infer_pb2.Result(id=request.id, label=label, infer_ms=infer_ms)

async def main():
    opts = [
        ('grpc.max_send_message_length', 50*1024*1024),
        ('grpc.max_receive_message_length', 50*1024*1024),
    ]
    server = grpc.aio.server(options=opts)
    infer_pb2_grpc.add_InferenceServicer_to_server(InferenceServicer(), server)
    server.add_insecure_port(BIND)
    log.info(f"gRPC on {BIND}")
    await server.start()
    await server.wait_for_termination()

if __name__ == "__main__":
    asyncio.run(main())
