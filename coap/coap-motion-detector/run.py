import logging
import asyncio
from motion_detection.detector import MotionDetector
from coap.server import start_server


logging.basicConfig(
    level=logging.INFO,
    format="%%(asctime)s %(levelname)s %(name)s: %(message)s"
)


# configure here
BIND_IP = "192.168.8.222"
BIND_PORT = 5683

def main():
    det = MotionDetector(
        camera_index=0,
        min_contour_area=2500,
        pad_ratio=0.12,
        show_windows=True,      # <- OFF: no contour/preview windows
        cooldown_sec=1.0,
        quiet_sec=3.0,
        sleep_sec=0.04
    )

    asyncio.run(start_server(
            bind_ip=BIND_IP,
            bind_port=BIND_PORT,
            motion_detector_loop=det.loop
        )
    )

if __name__ == "__main__":
    main()
