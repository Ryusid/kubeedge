#!/usr/bin/env python3
import os
import logging
from datetime import datetime

from mqtt_client import MqttClient
from motion_detector import MotionDetector

logging.basicConfig(
    level=os.environ.get("LOG_LEVEL", "INFO"),
    format="%(asctime)s %(levelname)s %(name)s: %(message)s"
)
log = logging.getLogger("app")

def env(k, d):
    return os.environ.get(k, d)

def main():
    # Topics
    topic_base = env("TOPIC_BASE", "motion/device/mqtt-sensor-room1/").rstrip("/") + "/"
    t_state = topic_base + "state"
    t_last  = topic_base + "last_detection"
    t_image = topic_base + "image"

    # MQTT
    mq = MqttClient()
    mq.start()
    log.info("publishing to base='%s' (state,last_detection,image)", topic_base)

    # Motion detector callbacks
    def on_true_with_image(ts_str: str, jpeg: bytes, size_b: int):
        mq.publish(t_state, "true", qos=1)
        mq.publish(t_last, ts_str, qos=1)
        mq.publish(t_image, jpeg, qos=0)
        log.info("[MQTT] TRUE + last_detection(%s) + image(%dB)", ts_str, size_b)

    def on_image(ts_str: str, jpeg: bytes, size_b: int):
        mq.publish(t_last, ts_str, qos=1)
        mq.publish(t_image, jpeg, qos=0)
        log.info("[MQTT] image during ACTIVE (%dB)", size_b)

    def on_false():
        mq.publish(t_state, "false", qos=1)
        log.info("[MQTT] FALSE")

    # Run
    detector = MotionDetector()
    try:
        detector.run(on_true_with_image, on_image, on_false)
    except KeyboardInterrupt:
        log.info("Interrupted by user")
    finally:
        mq.stop()
        log.info("shutdown complete")

if __name__ == "__main__":
    main()
