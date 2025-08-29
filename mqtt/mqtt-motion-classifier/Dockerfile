FROM python:3.11-slim

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py .
COPY yolo11n.pt .

ENV MQTT_BROKER=192.168.8.218 \
    MQTT_PORT=1883 \
    TOPIC_IN=motion/device/+/image \
    TOPIC_OUT_FMT=motion/device/{id}/class

CMD ["python", "main.py"]
