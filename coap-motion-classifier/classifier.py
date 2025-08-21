import asyncio
from aiocoap import Context, Message, Code
from aiocoap.numbers.codes import GET, PUT
from aiocoap import resource

CAMERA = "coap://192.168.8.222:5683"  # CoAP server
PATH_LAST = f"{CAMERA}/lastdetection"
PATH_IMAGE = f"{CAMERA}/image"
PATH_CLASS = f"{CAMERA}/class"

async def infer_jpeg(jpeg_bytes: bytes) -> str:
    # for now no model is uploaded it will always return unknown
    return "unknown"

async def handle_notification(ctx, msg):
    # A new timestamp means a fresh ROI is available
    # 1) Pull image
    img = await ctx.request(Message(code=GET, uri=PATH_IMAGE)).response
    label = await infer_jpeg(img.payload)
    # 2) Push label
    await ctx.request(Message(code=PUT, uri=PATH_CLASS,
                              payload=label.encode('utf-8'))).response
    print("classified:", label)

async def main():
    ctx = await Context.create_client_context()
    # Observe last_detection
    pr = ctx.request(Message(code=GET, uri=PATH_LAST, observe=0))
    r = await pr.response  # initial
    print("initial last_detection:", r.payload.decode())

    def cb(_r):
        asyncio.create_task(handle_notification(ctx, _r))

    pr.observation.register_callback(cb)
    pr.observation.register_errback(lambda e: print("observe error:", e))

    await asyncio.get_running_loop().create_future()

if __name__ == "__main__":
    asyncio.run(main())
