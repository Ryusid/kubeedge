#!/usr/bin/env python3
import os
import time
import logging
from datetime import datetime
from typing import Callable, Optional, Tuple

import cv2
import numpy as np

log = logging.getLogger("motion-detector")

def env(k, d):
    return os.environ.get(k, d)

def encode_jpeg(bgr_img, quality: int) -> Optional[bytes]:
    ok, buf = cv2.imencode(".jpg", bgr_img, [int(cv2.IMWRITE_JPEG_QUALITY), int(quality)])
    return buf.tobytes() if ok else None

class MotionDetector:
    """
    Detects motion, emits events:
      - on_true_with_image(ts_str, jpeg_bytes, size_bytes)
      - on_image(ts_str, jpeg_bytes, size_bytes)
      - on_false()
    """
    def __init__(self):
        # Tunables (env overrides)
        self.min_contour_area = int(env("MIN_CONTOUR_AREA", "4000"))
        self.pad_ratio        = float(env("PAD_RATIO", "0.12"))
        self.jpeg_quality     = int(env("JPEG_QUALITY", "85"))

        self.cooldown_sec     = float(env("COOLDOWN_SEC", "1.0"))
        self.quiet_sec        = float(env("QUIET_SEC", "6.0"))
        self.max_event_sec    = float(env("MAX_EVENT_SEC", "30.0"))
        self.img_interval_sec = float(env("IMG_INTERVAL_SEC", "3.0"))
        self.preview          = env("PREVIEW", "0") == "1"
        self.camera_index     = int(env("CAMERA_INDEX", "0"))

        # Background subtractor + morphology
        self.backSub = cv2.createBackgroundSubtractorMOG2(
            history=500, varThreshold=64, detectShadows=True
        )
        self.kernel = cv2.getStructuringElement(cv2.MORPH_RECT, (3, 3))

        # State
        self.state = "IDLE"
        self.last_event_ts = 0.0
        self.last_motion_ts = 0.0
        self.last_img_ts = 0.0

        self.cap = None

    def _largest_contour(self, mask) -> Optional[np.ndarray]:
        cnts, _ = cv2.findContours(mask, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_SIMPLE)
        best, best_a = None, 0.0
        for c in cnts:
            a = cv2.contourArea(c)
            if a > self.min_contour_area and a > best_a:
                best, best_a = c, a
        return best

    def _crop_with_pad(self, frame, rect) -> Optional[np.ndarray]:
        x, y, w, h = rect
        pad = int(max(w, h) * self.pad_ratio)
        H, W = frame.shape[:2]
        x0 = max(0, x - pad); y0 = max(0, y - pad)
        x1 = min(W, x + w + pad); y1 = min(H, y + h + pad)
        crop = frame[y0:y1, x0:x1].copy()
        return crop if crop.size > 0 else None

    def _process_frame(self, frame) -> Tuple[Optional[np.ndarray], Optional[Tuple[int,int,int,int]]]:
        fg = self.backSub.apply(frame, learningRate=0.001)
        _, mask = cv2.threshold(fg, 200, 255, cv2.THRESH_BINARY)
        mask = cv2.morphologyEx(mask, cv2.MORPH_OPEN, self.kernel, iterations=1)
        mask = cv2.dilate(mask, self.kernel, iterations=2)
        c = self._largest_contour(mask)
        rect = cv2.boundingRect(c) if c is not None else None
        return (mask if self.preview else None), rect

    def run(self,
            on_true_with_image: Callable[[str, bytes, int], None],
            on_image: Callable[[str, bytes, int], None],
            on_false: Callable[[], None]):

        self.cap = cv2.VideoCapture(self.camera_index)
        time.sleep(2)
        ok, frame = self.cap.read()
        if not ok:
            raise RuntimeError("Camera read failed")

        try:
            while True:
                ok, frame = self.cap.read()
                if not ok:
                    break

                _, rect = self._process_frame(frame)
                motion_now = rect is not None
                now = time.time()

                if motion_now:
                    self.last_motion_ts = now

                if self.state == "IDLE":
                    if motion_now and (now - self.last_event_ts) >= self.cooldown_sec:
                        crop = self._crop_with_pad(frame, rect)
                        if crop is not None:
                            jpeg = encode_jpeg(crop, self.jpeg_quality)
                            if jpeg:
                                ts = datetime.now().strftime("%Y-%m-%d_%H-%M-%S")
                                on_true_with_image(ts, jpeg, len(jpeg))
                                self.state = "ACTIVE"
                                self.last_event_ts = now
                                self.last_img_ts = now
                else:
                    quiet_long_enough = (now - self.last_motion_ts) >= self.quiet_sec
                    over_cap = (now - self.last_event_ts) >= self.max_event_sec

                    if motion_now and (now - self.last_img_ts) >= self.img_interval_sec:
                        crop = self._crop_with_pad(frame, rect)
                        if crop is not None:
                            jpeg = encode_jpeg(crop, self.jpeg_quality)
                            if jpeg:
                                ts = datetime.now().strftime("%Y-%m-%d_%H-%M-%S")
                                on_image(ts, jpeg, len(jpeg))
                                self.last_img_ts = now

                    if quiet_long_enough or over_cap:
                        on_false()
                        self.state = "IDLE"

                if self.preview:
                    if rect is not None:
                        x, y, w, h = rect
                        pad = int(max(w, h) * self.pad_ratio)
                        x0 = max(0, x - pad); y0 = max(0, y - pad)
                        x1 = min(frame.shape[1], x + w + pad); y1 = min(frame.shape[0], y + h + pad)
                        cv2.rectangle(frame, (x0, y0), (x1, y1), (0, 255, 0), 2)
                    cv2.imshow("Motion (MOG2)", frame)
                    if cv2.waitKey(1) & 0xFF == 27:  # ESC
                        break
        finally:
            self.cap.release()
            if self.preview:
                cv2.destroyAllWindows()
