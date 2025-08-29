#!/usr/bin/env python3
import asyncio
import logging
import os
import numpy as np
import cv2
import random
from aiocoap import Context, Message, Code


logging.basicConfig(level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s: %(message)s")


log = logging.getLogger("Coap-classifier")

def env(key, default):
    return os.environ.get(key, default)

HOST        = env("COAP_HOST", "192.168.8.222")
PORT        = int(env("COAP_PORT", "5683"))
SIGNAL_PATH = env("SIGNAL_PATH", "signal")
IMAGE_PATH  = env("IMAGE_PATH",  "image")
CLASS_PATH  = env("CLASS_PATH",  "class")

INFER_DELAY_MS = int(env("INFER_DELAY_MS", "10"))      # simulate inference 10ms
RETRY_MS       = int(env("RETRY_MS", "2000"))          # backoff when server down
LABEL_COUNT    = int(env("LABEL_COUNT", "5"))          # choose 5 or 9, etc.
# Optional: comma-separated names overrides numeric labels
CUSTOM_NAMES   = env("CLASS_NAMES", "").strip()

if CUSTOM_NAMES:
    LABELS = [n.strip() for n in CUSTOM_NAMES.split(",") if n.strip()]
else:
    LABELS = [f"label_{i}" for i in range(LABEL_COUNT)]

def uri(path: str) -> str:
    return f"coap://{HOST}:{PORT}/{path}"

async def handle_signal(protocol: Context):
    # Start an observation on /signal
    log.info(f"observing {uri(SIGNAL_PATH)}")
    req = Message(code=Code.GET, uri=uri(SIGNAL_PATH), observe=0)
    requester = protocol.request(req)

    try:
        first = await requester.response  # initial state
        log.info(f"initial signal: {first.payload!r}")

        async for notif in requester.observation:  # subsequent notifications
            seq = notif.payload.decode(errors="ignore")
            log.info(f"signal update: seq={seq}")

            # 1) Fetch latest image
            try:
                img_res = await protocol.request(Message(code=Code.GET, uri=uri(IMAGE_PATH))).response
                if img_res.code.is_successful():
                    img_bytes = img_res.payload
                else:
                    print(f"[classifier] /image GET failed: {img_res.code}")
                    continue
            except Exception as e:
                print(f"[classifier] /image GET error: {e}")
                continue
            # 2) "Run" inference
            arr = np.frombuffer(img_bytes, dtype=np.uint8)
            img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
            await asyncio.sleep(INFER_DELAY_MS / 1000.0)
            # label = random.choice(LABELS).encode()
            label = seq
            # 3) PUT /class with the label
            out=f"{seq}:{label}".encode()
            try:
                put_res = await protocol.request(
                    Message(code=Code.PUT, uri=uri(CLASS_PATH), payload=out)
                ).response
                if put_res.code.is_successful():
                    log.info(f"[classifier] wrote label: {label.decode()}")
                else:
                    print(f"[classifier] PUT /class failed: {put_res.code}")
            except Exception as e:
                print(f"[classifier] PUT /class error: {e}")

    except Exception as e:
        # Observation couldn't be established
        print(f"observe error: {e}")
        raise
    finally:
        # Stop observing if we're leaving this coroutine
        if requester.observation is not None:
            requester.observation.cancel()

async def main_loop():
    while True:
        try:
            protocol = await Context.create_client_context()
            await handle_signal(protocol)
        except Exception as e:
            log.info(f"will retry in {RETRY_MS}ms (reason: {e})")
            await asyncio.sleep(RETRY_MS / 1000.0)
        finally:
            pass

if __name__ == "__main__":
    asyncio.run(main_loop())
