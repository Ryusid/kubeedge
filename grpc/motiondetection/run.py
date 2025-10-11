import logging
import asyncio
from motion_detection.detector import MotionDetector
from coap_server.server import start_server
import os

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s: %(message)s"
)

BIND_IP = "127.0.0.1"
BIND_PORT = 5683
GRPC_ADDR = os.getenv("GRPC_ADDR", "127.0.0.1:50051")  # <--- the address of the grpc dispatcher

def main():
    det = MotionDetector(
        camera_index=0,
        min_contour_area=2500,
        pad_ratio=0.12,
        show_windows=True,
        cooldown_sec=1.0,
        quiet_sec=3.0,
        sleep_sec=0.04
    )

    asyncio.run(start_server(
        bind_ip=BIND_IP,
        bind_port=BIND_PORT,
        motion_detector_loop=det.loop,
        grpc_addr=GRPC_ADDR
    ))

if __name__ == "__main__":
    main()
