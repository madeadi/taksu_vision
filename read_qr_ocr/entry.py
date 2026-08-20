"""Type of the per-crop dicts this package decodes/OCRs.

This package takes already-detected, already-straightened crop *files* as
input (see `load_crop_entries` in `decoder.py`) rather than running
detection itself, so this shape carries no detection-specific fields
(no box_index/xyxy/polygon/is_obb/layout_*) — just what's needed to
decode/OCR a standalone crop image.
"""

from __future__ import annotations

from typing import TypedDict

import numpy as np


class OcrResult(TypedDict):
    text: str
    confidence: float


class CropEntry(TypedDict):
    image: str
    image_name: str
    _crop_path: str | None
    gray: np.ndarray

    decoded: list[str]
    decode_angle: float | None
    decode_method: str | None
    decode_attempts: int
    decode_failure_reason: str | None

    ocr: list[OcrResult]
    ocr_angle: int | None
    orientation_confident: bool
    orientation_margin: float | None
    orientation_source: str | None
