"""Crop geometry for axis-aligned and oriented detections."""

from __future__ import annotations

import cv2
import numpy as np


def axis_aligned_envelope(points: np.ndarray) -> list[float]:
    """Return the legacy xyxy envelope around four corner points."""
    quad = np.asarray(points, dtype=np.float32).reshape(4, 2)
    return [
        float(quad[:, 0].min()),
        float(quad[:, 1].min()),
        float(quad[:, 0].max()),
        float(quad[:, 1].max()),
    ]


def axis_aligned_crop(
    image: np.ndarray,
    xyxy: list[float],
    pad_ratio: float = 0.15,
) -> np.ndarray:
    """Crop a legacy xyxy detection with proportional padding."""
    height, width = image.shape[:2]
    x1, y1, x2, y2 = xyxy
    box_width, box_height = x2 - x1, y2 - y1
    pad_x, pad_y = box_width * pad_ratio, box_height * pad_ratio
    left = max(0, int(x1 - pad_x))
    top = max(0, int(y1 - pad_y))
    right = min(width, int(x2 + pad_x))
    bottom = min(height, int(y2 + pad_y))
    return image[top:bottom, left:right]


def order_quad_points(points: np.ndarray) -> np.ndarray:
    """Order a convex quadrilateral as top-left, top-right, bottom-right, bottom-left."""
    quad = np.asarray(points, dtype=np.float32).reshape(4, 2)
    center = quad.mean(axis=0)
    angles = np.arctan2(quad[:, 1] - center[1], quad[:, 0] - center[0])
    ordered = quad[np.argsort(angles)]
    top_left_index = int(np.argmin(ordered.sum(axis=1)))
    return np.roll(ordered, -top_left_index, axis=0)


def expand_quad(
    points: np.ndarray,
    pad_ratio: float,
    image_shape: tuple[int, ...],
) -> np.ndarray:
    """Expand a quadrilateral around its center and keep it inside the image."""
    quad = np.asarray(points, dtype=np.float32).reshape(4, 2)
    center = quad.mean(axis=0)
    expanded = center + (quad - center) * (1.0 + 2.0 * pad_ratio)
    height, width = image_shape[:2]
    expanded[:, 0] = np.clip(expanded[:, 0], 0, max(0, width - 1))
    expanded[:, 1] = np.clip(expanded[:, 1], 0, max(0, height - 1))
    return expanded.astype(np.float32)


def rectify_obb_crop(
    image: np.ndarray,
    points: np.ndarray,
    pad_ratio: float = 0.15,
) -> np.ndarray:
    """Perspective-warp an OBB into a canonical crop with its long side horizontal."""
    ordered = order_quad_points(expand_quad(points, pad_ratio, image.shape))
    top_left, top_right, bottom_right, bottom_left = ordered

    output_width = int(
        round(
            max(
                np.linalg.norm(top_right - top_left),
                np.linalg.norm(bottom_right - bottom_left),
            )
        )
    )
    output_height = int(
        round(
            max(
                np.linalg.norm(bottom_left - top_left),
                np.linalg.norm(bottom_right - top_right),
            )
        )
    )
    if output_width < 2 or output_height < 2:
        return np.empty((0, 0), dtype=image.dtype)

    destination = np.array(
        [
            [0, 0],
            [output_width - 1, 0],
            [output_width - 1, output_height - 1],
            [0, output_height - 1],
        ],
        dtype=np.float32,
    )
    transform = cv2.getPerspectiveTransform(ordered, destination)
    crop = cv2.warpPerspective(
        image,
        transform,
        (output_width, output_height),
        flags=cv2.INTER_LINEAR,
        borderMode=cv2.BORDER_REPLICATE,
    )

    if crop.shape[0] > crop.shape[1]:
        crop = cv2.rotate(crop, cv2.ROTATE_90_CLOCKWISE)
    return crop


def normalize_label_direction(crop: np.ndarray) -> tuple[np.ndarray, int, float]:
    """Put the dense Data Matrix end of a horizontal label on the left.

    OBB geometry can normalize the long axis, but it cannot distinguish 0°
    from 180°. These labels have a stable layout: the Data Matrix is the
    highest-detail square at the left end and the human-readable text follows.
    """
    if crop.size == 0 or crop.shape[1] <= crop.shape[0]:
        return crop, 0, 0.0

    gray = (
        cv2.cvtColor(crop, cv2.COLOR_BGR2GRAY)
        if crop.ndim == 3
        else crop
    )
    height, width = gray.shape[:2]
    band_width = max(height, min(width // 3, int(round(height * 1.35))))

    def detail_score(region: np.ndarray) -> float:
        gradient_x = cv2.Sobel(region, cv2.CV_32F, 1, 0, ksize=3)
        gradient_y = cv2.Sobel(region, cv2.CV_32F, 0, 1, ksize=3)
        return float((np.abs(gradient_x) + np.abs(gradient_y)).mean())

    left_score = detail_score(gray[:, :band_width])
    right_score = detail_score(gray[:, width - band_width :])
    denominator = max(left_score, right_score, 1.0)
    margin = abs(left_score - right_score) / denominator

    # Avoid changing ambiguous/partial crops where neither end clearly contains
    # the Data Matrix. OCR remains available for those cases.
    if right_score > left_score and margin >= 0.08:
        return cv2.rotate(crop, cv2.ROTATE_180), 180, margin
    return crop, 0, margin
