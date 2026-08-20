"""Gemini-based OCR for already-cropped image files."""

from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from google import genai
from google.genai import types

IMG_EXTS = {".jpg", ".jpeg", ".png", ".bmp", ".webp"}

DEFAULT_PROMPT = (
    "Read all text visible in this image. Return only the text you see, "
    "exactly as it appears, with no commentary, labels, or formatting. "
    "If no text is visible, return an empty string."
)

_MIME_TYPES = {
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".png": "image/png",
    ".bmp": "image/bmp",
    ".webp": "image/webp",
}


def _ocr_one(client: genai.Client, image_path: Path, model: str, prompt: str) -> dict:
    """OCR a single image file with Gemini.

    Never raises: a failure (bad image, API error, ...) is captured in the
    returned dict's `error` field instead, so one bad image doesn't abort
    the rest of a batch.
    """
    result = {
        "image": str(image_path),
        "image_name": image_path.name,
        "text": None,
        "error": None,
    }
    try:
        image_bytes = image_path.read_bytes()
        mime_type = _MIME_TYPES.get(image_path.suffix.lower(), "image/jpeg")
        response = client.models.generate_content(
            model=model,
            contents=[
                types.Part.from_bytes(data=image_bytes, mime_type=mime_type),
                prompt,
            ],
        )
        result["text"] = (response.text or "").strip()
    except Exception as exc:
        result["error"] = str(exc)
    return result


def ocr_images(
    client: genai.Client,
    image_paths: list[Path],
    model: str,
    prompt: str = DEFAULT_PROMPT,
    max_concurrency: int = 4,
) -> list[dict]:
    """OCR each image in `image_paths` with Gemini.

    Requests run in parallel via a thread pool (each call is a network-bound
    API request, so threads overlap I/O wait rather than compete for the
    GIL). Returns one `{image, image_name, text, error}` dict per input
    path, in the same order as `image_paths`.
    """
    if not image_paths:
        return []
    with ThreadPoolExecutor(max_workers=max(1, max_concurrency)) as pool:
        return list(pool.map(lambda p: _ocr_one(client, p, model, prompt), image_paths))
