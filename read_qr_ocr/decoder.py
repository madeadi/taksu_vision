"""Barcode decoding and OCR fallback for rectified label crops."""

from __future__ import annotations

import re
from pathlib import Path

import cv2
import zxingcpp

from entry import CropEntry

# Preserve the formats used by the existing pipeline output behavior.
BARCODE_FORMATS = (
    zxingcpp.BarcodeFormat.DataMatrix
    | zxingcpp.BarcodeFormat.Aztec
)
CLAHE = cv2.createCLAHE(clipLimit=2.0, tileGridSize=(8, 8))
SERIAL_STRICT = re.compile(r"\b[A-Z]{2}\d{2}-\d{5}\b")
SERIAL_LOOSE = re.compile(r"\b[A-Z0-9]{4}-[A-Z0-9]{5}\b")


def _rotate_counterclockwise(image, angle):
    """Rotate an image counterclockwise by a right angle."""
    normalized = int(round(float(angle) / 90.0) * 90) % 360
    rotations = {
        0: None,
        90: cv2.ROTATE_90_COUNTERCLOCKWISE,
        180: cv2.ROTATE_180,
        270: cv2.ROTATE_90_CLOCKWISE,
    }
    rotation = rotations.get(normalized)
    if rotation is None:
        return image
    return cv2.rotate(image, rotation)


def _normalize_entry_orientation(entry: CropEntry, angle: float) -> None:
    """Rotate the in-memory and saved crop into its decoded upright direction."""
    normalized = int(round(float(angle) / 90.0) * 90) % 360
    if normalized == 0 or normalized not in {90, 180, 270}:
        return

    entry["gray"] = _rotate_counterclockwise(entry["gray"], normalized)

    crop_path = entry.get("_crop_path")
    if not crop_path:
        return
    crop = cv2.imread(str(crop_path))
    if crop is None:
        return
    cv2.imwrite(
        str(crop_path),
        _rotate_counterclockwise(crop, normalized),
        [cv2.IMWRITE_JPEG_QUALITY, 95],
    )


def _decode_variants(gray, upscale_factor):
    """Yield inexpensive-to-expensive image variants for difficult symbols."""
    yield "raw", gray
    yield "clahe", CLAHE.apply(gray)

    if upscale_factor > 1:
        for name, interpolation in (
            ("upscale_nearest", cv2.INTER_NEAREST),
            ("upscale_cubic", cv2.INTER_CUBIC),
        ):
            yield name, cv2.resize(
                gray,
                None,
                fx=upscale_factor,
                fy=upscale_factor,
                interpolation=interpolation,
            )

    working = (
        cv2.resize(
            gray,
            None,
            fx=upscale_factor,
            fy=upscale_factor,
            interpolation=cv2.INTER_CUBIC,
        )
        if upscale_factor > 1
        else gray
    )
    enhanced = CLAHE.apply(working)
    blurred = cv2.GaussianBlur(enhanced, (0, 0), 1.0)
    yield "clahe_sharpen", cv2.addWeighted(enhanced, 1.7, blurred, -0.7, 0)

    _threshold, otsu = cv2.threshold(
        enhanced, 0, 255, cv2.THRESH_BINARY | cv2.THRESH_OTSU
    )
    yield "otsu", otsu
    yield "otsu_inverted", cv2.bitwise_not(otsu)

    block_size = max(3, int(round(min(enhanced.shape[:2]) / 8)) | 1)
    # Very large blocks can erase small Data Matrix modules.
    block_size = min(block_size, 51)
    adaptive = cv2.adaptiveThreshold(
        enhanced,
        255,
        cv2.ADAPTIVE_THRESH_GAUSSIAN_C,
        cv2.THRESH_BINARY,
        block_size,
        5,
    )
    yield "adaptive", adaptive
    yield "adaptive_inverted", cv2.bitwise_not(adaptive)


def load_crop_entries(crop_paths) -> list[CropEntry]:
    """Build `CropEntry` dicts by reading already-cropped image files from disk.

    Each crop is expected to already be straightened/rectified (e.g. by
    `image_crops.detect_and_crop`) — this just loads pixels, it does no
    detection or geometry correction of its own.
    """
    entries: list[CropEntry] = []
    for crop_path in crop_paths:
        crop_path = Path(crop_path)
        image = cv2.imread(str(crop_path))
        if image is None:
            print(f"WARNING: could not read {crop_path}")
            continue

        # Crops are always persisted as JPEG, even if the source file was a
        # different accepted format (e.g. .png/.bmp).
        if crop_path.suffix.lower() not in (".jpg", ".jpeg"):
            jpg_path = crop_path.with_suffix(".jpg")
            cv2.imwrite(str(jpg_path), image, [cv2.IMWRITE_JPEG_QUALITY, 95])
            crop_path.unlink()
            crop_path = jpg_path

        gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY) if image.ndim == 3 else image
        entries.append(
            {
                "image": str(crop_path),
                "image_name": crop_path.name,
                "_crop_path": str(crop_path),
                "gray": gray,
                "decoded": [],
                "decode_angle": None,
                "decode_method": None,
                "decode_attempts": 0,
                "decode_failure_reason": None,
                "ocr": [],
                "ocr_angle": None,
                "orientation_confident": False,
                "orientation_margin": None,
                "orientation_source": None,
            }
        )
    return entries


def decode_crops(entries: list[CropEntry], upscale_factor: float) -> None:
    """Decode each crop with a preprocessing ensemble before allowing OCR.

    Mutates each entry in place: `decode_method`, `decode_attempts`,
    `decode_failure_reason`, and on success `decoded`/`decode_angle`/
    `orientation_confident` (plus the crop's own orientation, if the barcode
    reports one). Expects `entry["gray"]` to already hold a grayscale crop —
    see `load_crop_entries`.
    """
    for entry in entries:
        entry["decode_method"] = None
        entry["decode_attempts"] = 0
        entry["decode_failure_reason"] = None

        for method, candidate in _decode_variants(entry["gray"], upscale_factor):
            entry["decode_attempts"] += 1
            results = zxingcpp.read_barcodes(candidate, formats=BARCODE_FORMATS)
            if not results:
                continue

            entry["decoded"] = list(dict.fromkeys(result.text for result in results))
            entry["decode_method"] = method
            orientation = getattr(results[0], "orientation", None)
            entry["decode_angle"] = orientation
            entry["orientation_confident"] = orientation is not None
            if orientation is not None:
                _normalize_entry_orientation(entry, orientation)
            break

        if not entry["decoded"]:
            height, width = entry["gray"].shape[:2]
            sharpness = cv2.Laplacian(entry["gray"], cv2.CV_64F).var()
            if min(height, width) < 32:
                entry["decode_failure_reason"] = "too_small"
            elif sharpness < 15:
                entry["decode_failure_reason"] = "blurred"
            else:
                entry["decode_failure_reason"] = "decode_exhausted"


def build_ocr_easyocr():
    import easyocr

    reader = easyocr.Reader(["en"], gpu=True)

    def run(image):
        return [
            {"text": text, "confidence": round(confidence, 4)}
            for _, text, confidence in reader.readtext(image)
        ]

    return run


def build_ocr_ocrmac():
    from PIL import Image
    from ocrmac import ocrmac

    def run(image):
        annotations = ocrmac.OCR(
            Image.fromarray(image), recognition_level="accurate"
        ).recognize()
        return [
            {"text": text, "confidence": round(confidence, 4)}
            for text, confidence, _bbox in annotations
        ]

    return run


OCR_ENGINES = {"easyocr": build_ocr_easyocr, "ocrmac": build_ocr_ocrmac}

_ocr_engine_cache: dict[str, object] = {}


def _get_ocr_engine(engine: str):
    """Build (and cache) the OCR runner for `engine`, so repeated calls don't
    pay model-load cost (e.g. easyocr.Reader loading its weights) again."""
    if engine not in _ocr_engine_cache:
        _ocr_engine_cache[engine] = OCR_ENGINES[engine]()
    return _ocr_engine_cache[engine]


def _ocr_score(results) -> float:
    """Score readable label text, strongly preferring a plausible serial."""
    confidence_score = sum(
        max(1, len(result["text"].strip())) * float(result["confidence"])
        for result in results
    )
    combined = " ".join(result["text"].upper() for result in results)
    alphanumeric = sum(character.isalnum() for character in combined)
    junk = sum(
        not (character.isalnum() or character in " -/,.;:")
        for character in combined
    )
    serial_bonus = 100.0 if SERIAL_STRICT.search(combined) else 0.0
    if not serial_bonus and SERIAL_LOOSE.search(combined):
        serial_bonus = 45.0
    return confidence_score + min(alphanumeric, 40) * 0.15 + serial_bonus - junk * 2


def ocr_failed_crops(
    entries: list[CropEntry], engine: str, preprocess: bool, scale: float
) -> list[CropEntry]:
    """Run OCR at all right-angle orientations on decode failures."""
    failed = [entry for entry in entries if not entry["decoded"]]
    if not failed:
        return failed

    run_ocr = _get_ocr_engine(engine)
    for entry in failed:
        image = entry["gray"]
        if preprocess:
            upscaled = cv2.resize(
                image,
                None,
                fx=scale,
                fy=scale,
                interpolation=cv2.INTER_CUBIC,
            )
            image = CLAHE.apply(upscaled)

        # No is_obb signal available (crops arrive pre-straightened, not
        # detection metadata), so always test all four right angles.
        candidates = [
            (angle, run_ocr(_rotate_counterclockwise(image, angle)))
            for angle in (0, 90, 180, 270)
        ]
        ranked = sorted(
            candidates,
            key=lambda candidate: _ocr_score(candidate[1]),
            reverse=True,
        )
        best_angle, entry["ocr"] = ranked[0]
        best_score = _ocr_score(ranked[0][1])
        runner_up_score = _ocr_score(ranked[1][1])
        margin = best_score - runner_up_score
        orientation_confident = margin >= max(3.0, abs(best_score) * 0.08)
        entry["ocr_angle"] = best_angle
        entry["orientation_confident"] = orientation_confident
        entry["orientation_margin"] = round(margin, 3)
        if orientation_confident:
            entry["orientation_source"] = "ocr"
            _normalize_entry_orientation(entry, best_angle)

    return failed
