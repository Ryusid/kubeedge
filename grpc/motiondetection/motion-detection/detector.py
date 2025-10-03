import time
import cv2

class MotionDetector:
    """
    Simple MOG2-based motion detector.
    Calls the provided callbacks:
      - on_rise(crop_bgr)
      - on_fall()
    """
    def __init__(
        self,
        camera_index=0,
        min_contour_area=1200,
        pad_ratio=0.12,
        show_windows=False,
        cooldown_sec=1.0,      # min gap between motion TRUE events
        quiet_sec=3.0,         # no motion this long => FALSE
        sleep_sec=0.04
    ):
        self.camera_index = camera_index
        self.min_contour_area = min_contour_area
        self.pad_ratio = pad_ratio
        self.show = show_windows
        self.cooldown = cooldown_sec
        self.quiet = quiet_sec
        self.sleep = sleep_sec

        self.bg = cv2.createBackgroundSubtractorMOG2(history=500, varThreshold=64, detectShadows=True)
        self.kernel = cv2.getStructuringElement(cv2.MORPH_RECT, (3, 3))

        # state
        self.motion_on = False
        self.last_motion_seen_ts = 0.0   # last time any motion was detected
        self.last_rise_ts = 0.0          # last time we announced TRUE

    def _largest_contour(self, mask, min_area):
        cnts, _ = cv2.findContours(mask, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_SIMPLE)
        best, best_a = None, 0
        for c in cnts:
            a = cv2.contourArea(c)
            if a > min_area and a > best_a:
                best, best_a = c, a
        return best

    def _crop_with_pad(self, frame, rect):
        x, y, w, h = rect
        pad = int(max(w, h) * self.pad_ratio)
        H, W = frame.shape[:2]
        x0 = max(0, x - pad); y0 = max(0, y - pad)
        x1 = min(W, x + w + pad); y1 = min(H, y + h + pad)
        return frame[y0:y1, x0:x1].copy(), (x0, y0, x1, y1)

    def loop(self, on_rise, on_fall):
        cap = cv2.VideoCapture(self.camera_index)
        if not cap.isOpened():
            raise RuntimeError(f"Cannot open camera index {self.camera_index}")

        try:
            while True:
                ok, frame = cap.read()
                if not ok:
                    break

                # foreground mask (ignore shadows)
                fg = self.bg.apply(frame, learningRate=0.001)
                _, mask = cv2.threshold(fg, 200, 255, cv2.THRESH_BINARY)
                mask = cv2.morphologyEx(mask, cv2.MORPH_OPEN, self.kernel, iterations=1)
                mask = cv2.dilate(mask, self.kernel, iterations=2)

                big = self._largest_contour(mask, self.min_contour_area)
                motion_now = big is not None
                now = time.time()

                if motion_now:
                    self.last_motion_seen_ts = now

                # RISING EDGE (use last_rise_ts for cooldown — not last_motion_seen_ts)
                if not self.motion_on and motion_now and (now - self.last_rise_ts >= self.cooldown):
                    self.motion_on = True
                    self.last_rise_ts = now
                    crop = None
                    if big is not None:
                        crop, rect = self._crop_with_pad(frame, cv2.boundingRect(big))
                    on_rise(crop)

                # FALLING EDGE
                if self.motion_on and (now - self.last_motion_seen_ts >= self.quiet):
                    self.motion_on = False
                    on_fall()

                if self.show:
                    if big is not None:
                        x, y, w, h = cv2.boundingRect(big)
                        x0, y0, x1, y1 = self._crop_with_pad(frame, (x, y, w, h))[1]
                        cv2.rectangle(frame, (x0, y0), (x1, y1), (0, 255, 0), 2)
                    cv2.imshow('Motion', frame)
                    if cv2.waitKey(1) & 0xFF == 27:
                        break

                time.sleep(self.sleep)
        finally:
            cap.release()
            if self.show:
                cv2.destroyAllWindows()
