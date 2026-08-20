"""YOLO inference and OBB-aware crop extraction."""

from __future__ import annotations

import time
from pathlib import Path

import cv2

from geometry import (
    axis_aligned_crop,
    axis_aligned_envelope,
    normalize_label_direction,
    order_quad_points,
    rectify_obb_crop,
)

IMG_EXTS = {".jpg", ".jpeg", ".png", ".bmp"}


def blur_score(image) -> float:
    """Return variance of the Laplacian; lower values indicate blur."""
    gray = (
        cv2.cvtColor(image, cv2.COLOR_BGR2GRAY)
        if image.ndim == 3
        else image
    )
    return float(cv2.Laplacian(gray, cv2.CV_64F).var())


def detect_and_crop(
    model,
    image_paths,
    conf,
    pad_ratio,
    save_crops_dir,
    blur_thresh=0.0,
    timing=None,
    device=None,
):
    """Run YOLO and create rectified crops while retaining the legacy result shape.

    If `timing` is a dict, accumulates `load_seconds` (time spent reading the
    image and computing its blur score, before detection), `detect_seconds`
    (time spent in `model.predict`), and `crop_seconds`/`crop_count` (time
    spent rectifying/padding + saving each box) into it.

    `device` is passed straight through to `model.predict` (e.g. "mps",
    "cpu", "cuda:0"); leave it `None` to use ultralytics' own auto-detection.

    Returns `(entries, skipped)`, where `skipped` is a list of
    `{"image", "image_name", "reason", ...}` — one entry per whole image that
    was never detected on, with `reason` one of `"unreadable"` (cv2.imread
    failed) or `"blurry"` (below `blur_thresh`, with `blur_score`/
    `blur_threshold` included for context).
    """
    entries = []
    skipped = []

    if save_crops_dir:
        save_crops_dir = Path(save_crops_dir)
        save_crops_dir.mkdir(parents=True, exist_ok=True)

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
                crop_start = time.perf_counter()
                if is_obb:
                    points = box.xyxyxyxy[0].detach().cpu().numpy()
                    polygon = order_quad_points(points).tolist()
                    xyxy = axis_aligned_envelope(points)
                    crop = rectify_obb_crop(image, points, pad_ratio=pad_ratio)
                    crop, layout_angle, layout_margin = normalize_label_direction(crop)
                else:
                    xyxy = box.xyxy[0].detach().cpu().tolist()
                    x1, y1, x2, y2 = xyxy
                    polygon = [[x1, y1], [x2, y1], [x2, y2], [x1, y2]]
                    crop = axis_aligned_crop(image, xyxy, pad_ratio=pad_ratio)
                    layout_angle, layout_margin = 0, 0.0

                if crop.size == 0:
                    continue

                crop_path = None
                if save_crops_dir:
                    crop_name = f"{image_path.stem}_box{index}.jpg"
                    crop_path = save_crops_dir / crop_name
                    cv2.imwrite(
                        str(crop_path),
                        crop,
                        [cv2.IMWRITE_JPEG_QUALITY, 85],
                    )

                gray = (
                    cv2.cvtColor(crop, cv2.COLOR_BGR2GRAY)
                    if crop.ndim == 3
                    else crop
                )
                if timing is not None:
                    timing["crop_seconds"] = (
                        timing.get("crop_seconds", 0.0) + time.perf_counter() - crop_start
                    )
                    timing["crop_count"] = timing.get("crop_count", 0) + 1
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
                        "_crop_path": str(crop_path) if crop_path else None,
                        "gray": gray,
                        "decoded": [],
                        "decode_angle": None,
                        "decode_method": None,
                        "decode_attempts": 0,
                        "decode_failure_reason": None,
                        "layout_angle": layout_angle,
                        "layout_margin": round(layout_margin, 3),
                        "ocr": [],
                        "ocr_angle": None,
                        "orientation_confident": False,
                        "orientation_margin": None,
                        "orientation_source": "layout" if layout_margin >= 0.08 else None,
                    }
                )

    return entries, skipped
