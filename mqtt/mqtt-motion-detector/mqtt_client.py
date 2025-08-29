#!/usr/bin/env python3
import os
import time
import logging
import paho.mqtt.client as mqtt

log = logging.getLogger("mqtt-client")

def env(k, d):
    return os.environ.get(k, d)

class MqttClient:
    def __init__(self,
                 host: str | None = None,
                 port: int | None = None,
                 client_id: str | None = None,
                 keepalive: int = 30,
                 qos_default: int = 1):
        self.host = host or env("MQTT_BROKER_HOST", "192.168.8.218")
        self.port = port or int(env("MQTT_BROKER_PORT", "1883"))
        self.keepalive = keepalive
        self.qos_default = qos_default

        self.client = mqtt.Client(client_id=client_id)
        self.client.enable_logger(log)
        # exponential backoff on reconnect
        self.client.reconnect_delay_set(min_delay=1, max_delay=10)

        self.client.on_connect = self._on_connect
        self.client.on_disconnect = self._on_disconnect

    def _on_connect(self, c, u, flags, rc, props=None):
        log.info("connected rc=%s to %s:%s", rc, self.host, self.port)

    def _on_disconnect(self, c, u, rc, props=None):
        log.warning("disconnected rc=%s; will try to reconnect", rc)

    def start(self):
        # connect asynchronously so loop thread can auto-reconnect
        self.client.connect_async(self.host, self.port, self.keepalive)
        self.client.loop_start()

    def stop(self):
        try:
            self.client.loop_stop()
        finally:
            try:
                self.client.disconnect()
            except Exception:
                pass

    def publish(self, topic: str, payload: bytes | str,
                qos: int | None = None, retain: bool = False):
        q = self.qos_default if qos is None else qos
        res = self.client.publish(topic, payload=payload, qos=q, retain=retain)
        # Optionally wait for QoS1/2 ack:
        if q > 0:
            res.wait_for_publish(timeout=5)
        return res
