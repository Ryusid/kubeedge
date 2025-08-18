FROM python:3.11-slim

# OpenCV runtime deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    libglib2.0-0 libasound2 libsm6 libxext6 libxrender1 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py .

ENV MQTT_BROKER=192.168.8.218 \
    MQTT_PORT=1883 \
    TOPIC_IN=motion/device/+/image \
    TOPIC_OUT_FMT=motion/device/{id}/class

CMD ["python", "main.py"]
