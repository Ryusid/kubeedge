import os
import cv2
import numpy as np
import paho.mqtt.client as mqtt

BROKER   = os.environ.get("MQTT_BROKER", "192.168.8.218")
PORT     = int(os.environ.get("MQTT_PORT", "1883"))
TOPIC_IN = os.environ.get("TOPIC_IN", "motion/device/+/image")           # bytes (JPEG)
TOPIC_OUT_FMT = os.environ.get("TOPIC_OUT_FMT", "motion/device/{id}/class")

print(f"[CFG] broker={BROKER}:{PORT} in={TOPIC_IN}")

CASCADE_PATH = cv2.data.haarcascades + "haarcascade_frontalface_default.xml"
face_cascade = cv2.CascadeClassifier(CASCADE_PATH)
if face_cascade.empty():
   raise RuntimeError("Failed to load Haar cascade")

def classify_bgr(img_bgr: np.ndarray) -> str:
    """Return 'person' if we see a person, else 'unknown'."""
    if img_bgr is None or img_bgr.size == 0:
        return "unknown"
    else:
        # Very lightweight heuristic: faces ≈ person
        gray = cv2.cvtColor(img_bgr, cv2.COLOR_BGR2GRAY)
        faces = face_cascade.detectMultiScale(gray, 1.2, 3)
        return "person" if len(faces) > 0 else "unknown"

def on_message(client, userdata, msg):
    try:
        # topic: motion/device/<id>/image
        parts = msg.topic.split("/")
        cam_id = parts[2] if len(parts) >= 4 else "unknown"
        # decode JPEG bytes
        arr = np.frombuffer(msg.payload, dtype=np.uint8)
        img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
        label = str(classify_bgr(img))
        out_topic = TOPIC_OUT_FMT.format(id=cam_id)
        # publish label (qos=1 for reliability; no retain)
        client.publish(out_topic, label, qos=1, retain=False)
        print(f"[MQTT] {out_topic} <- {label}  (src: {msg.topic}, {len(msg.payload)}B)")
    except Exception as e:
        print(f"[ERR] processing {msg.topic}: {e}")

def main():
    client = mqtt.Client()
    client.connect(BROKER, PORT, 60)
    client.on_message = on_message
    client.subscribe(TOPIC_IN, qos=0)   # images can be QoS0
    print(f"[INIT] subscribed to {TOPIC_IN}")
    client.loop_forever()

if __name__ == "__main__":
    main()
