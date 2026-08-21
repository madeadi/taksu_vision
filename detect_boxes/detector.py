"""YOLO inference only — detects boxes, does no cropping."""

from __future__ import annotations

import time
from pathlib import Path

import cv2
import numpy as np

IMG_EXTS = {".jpg", ".jpeg", ".png", ".bmp"}


def blur_score(image) -> float:
    """Return variance of the Laplacian; lower values indicate blur."""
    gray = (
        cv2.cvtColor(image, cv2.COLOR_BGR2GRAY)
        if image.ndim == 3
        else image
    )
    return float(cv2.Laplacian(gray, cv2.CV_64F).var())


def order_quad_points(points: np.ndarray) -> np.ndarray:
    """Order a convex quadrilateral as top-left, top-right, bottom-right, bottom-left."""
    quad = np.asarray(points, dtype=np.float32).reshape(4, 2)
    center = quad.mean(axis=0)
    angles = np.arctan2(quad[:, 1] - center[1], quad[:, 0] - center[0])
    ordered = quad[np.argsort(angles)]
    top_left_index = int(np.argmin(ordered.sum(axis=1)))
    return np.roll(ordered, -top_left_index, axis=0)


def axis_aligned_envelope(points: np.ndarray) -> list[float]:
    """Return the axis-aligned xyxy envelope around four corner points."""
    quad = np.asarray(points, dtype=np.float32).reshape(4, 2)
    return [
        float(quad[:, 0].min()),
        float(quad[:, 1].min()),
        float(quad[:, 0].max()),
        float(quad[:, 1].max()),
    ]


def detect(
    model,
    image_paths,
    conf,
    blur_thresh=0.0,
    timing=None,
    device=None,
):
    """Run YOLO over `image_paths` and return detected boxes; no cropping.

    If `timing` is a dict, accumulates `load_seconds` (time spent reading the
    image and computing its blur score, before detection) and
    `detect_seconds` (time spent in `model.predict`) into it.

    `device` is passed straight through to `model.predict` (e.g. "mps",
    "cpu", "cuda:0"); leave it `None` to use ultralytics' own auto-detection.

    Returns `(entries, skipped)`. Each entry in `entries` is
    `{"image", "image_name", "box_index", "yolo_conf", "xyxy", "polygon",
    "is_obb"}` — `polygon` is the ordered 4-corner quad for OBB detections,
    or the `xyxy` box's 4 corners for plain detections. `skipped` is a list
    of `{"image", "image_name", "reason", ...}` — one entry per whole image
    that was never detected on, with `reason` one of `"unreadable"`
    (cv2.imread failed) or `"blurry"` (below `blur_thresh`, with
    `blur_score`/`blur_threshold` included for context).
    """
    entries = []
    skipped = []

    for image_path in image_paths:
        load_start = time.perf_counter()
        image_path = Path(image_path)
        image = cv2.imread(str(image_path))
        if image is None:
            print(f"WARNING: could not read {image_path}")
            skipped.append(
                {
                    "image": str(image_path),
                    "image_name": image_path.name,
                    "reason": "unreadable",
                }
            )
            continue

        if blur_thresh > 0:
            score = blur_score(image)
            if score < blur_thresh:
                print(
                    f"Skipping blurry {image_path.name} "
                    f"(blur score {score:.1f} < {blur_thresh:g})"
                )
                skipped.append(
                    {
                        "image": str(image_path),
                        "image_name": image_path.name,
                        "reason": "blurry",
                        "blur_score": round(score, 1),
                        "blur_threshold": blur_thresh,
                    }
                )
                continue

        if timing is not None:
            timing["load_seconds"] = (
                timing.get("load_seconds", 0.0) + time.perf_counter() - load_start
            )

        detect_start = time.perf_counter()
        results = model.predict(source=image, conf=conf, verbose=False, device=device)
        if timing is not None:
            timing["detect_seconds"] = (
                timing.get("detect_seconds", 0.0) + time.perf_counter() - detect_start
            )

        for result in results:
            is_obb = result.obb is not None
            boxes = result.obb if is_obb else result.boxes
            if boxes is None:
                continue

            for index, box in enumerate(boxes):
                if is_obb:
                    points = box.xyxyxyxy[0].detach().cpu().numpy()
                    polygon = order_quad_points(points).tolist()
                    xyxy = axis_aligned_envelope(points)
                else:
                    xyxy = box.xyxy[0].detach().cpu().tolist()
                    x1, y1, x2, y2 = xyxy
                    polygon = [[x1, y1], [x2, y1], [x2, y2], [x1, y2]]

                entries.append(
                    {
                        "image": str(image_path),
                        "image_name": image_path.name,
                        "box_index": index,
                        "yolo_conf": round(box.conf[0].item(), 4),
                        "xyxy": [round(value, 1) for value in xyxy],
                        "polygon": [
                            [round(float(x), 1), round(float(y), 1)]
                            for x, y in polygon
                        ],
                        "is_obb": is_obb,
                    }
                )

    return entries, skipped
