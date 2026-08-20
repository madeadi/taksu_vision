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
    "Return it as a single line, with each piece of text separated by a "
    "single space. If no text is visible, return an empty string."
)

_MIME_TYPES = {
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".png": "image/png",
    ".bmp": "image/bmp",
    ".webp": "image/webp",
}

# USD per 1M tokens, standard (non-batch) pricing as of 2026-08-21:
# https://ai.google.dev/gemini-api/docs/pricing
# gemini-2.5-pro's higher tier (>200k prompt tokens) isn't listed since a
# single cropped image never gets near that.
MODEL_PRICING = {
    "gemini-2.5-flash-lite": {"input": 0.10, "output": 0.40},
    "gemini-2.5-flash": {"input": 0.30, "output": 2.50},
    "gemini-2.5-pro": {"input": 1.25, "output": 10.00},
}


def _cost_usd(model: str, input_tokens: int, output_tokens: int) -> float | None:
    """USD cost for one request, or None if `model` isn't in MODEL_PRICING."""
    pricing = MODEL_PRICING.get(model)
    if pricing is None:
        return None
    return (input_tokens * pricing["input"] + output_tokens * pricing["output"]) / 1_000_000


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
        "input_tokens": None,
        "output_tokens": None,
        "cost_usd": None,
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
        result["text"] = " ".join((response.text or "").split())
        usage = response.usage_metadata
        if usage is not None:
            input_tokens = usage.prompt_token_count or 0
            # Thinking tokens are billed as output alongside the visible response.
            output_tokens = (usage.candidates_token_count or 0) + (usage.thoughts_token_count or 0)
            result["input_tokens"] = input_tokens
            result["output_tokens"] = output_tokens
            result["cost_usd"] = _cost_usd(model, input_tokens, output_tokens)
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
    GIL). Returns one `{image, image_name, text, input_tokens, output_tokens,
    cost_usd, error}` dict per input path, in the same order as
    `image_paths`.
    """
    if not image_paths:
        return []
    with ThreadPoolExecutor(max_workers=max(1, max_concurrency)) as pool:
        return list(pool.map(lambda p: _ocr_one(client, p, model, prompt), image_paths))
