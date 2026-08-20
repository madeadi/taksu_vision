"""FastAPI microservice: YOLO detection + OBB-aware crop rectification.

Loads the YOLO model once at startup so it stays warm across requests,
instead of paying model-load / MPS cold-start cost on every call.
"""

from __future__ import annotations

import json
import os
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from pathlib import Path

from fastapi import FastAPI, HTTPException, Query
from ultralytics import YOLO

from detector import IMG_EXTS, detect_and_crop

MODEL_PATH = os.environ.get("MODEL_PATH", "weight.pt")
DEVICE_ENV = os.environ.get("DEVICE")  # unset -> auto (mps on Apple Silicon, else cpu)
# Fixed at startup rather than a request param: a client-supplied path fed into
# cv2.imwrite would be an arbitrary-file-write vector.
SAVE_CROPS_DIR = Path(os.environ["SAVE_CROPS_DIR"]) if os.environ.get("SAVE_CROPS_DIR") else None

state: dict = {}


def resolve_device(explicit: str | None) -> str:
    if explicit:
        return explicit
    import torch

    return "mps" if torch.backends.mps.is_available() else "cpu"


@asynccontextmanager
async def lifespan(app: FastAPI):
    device = resolve_device(DEVICE_ENV)
    print(f"Loading model {MODEL_PATH} on device={device}")
    state["model"] = YOLO(MODEL_PATH)
    state["device"] = device
    yield
    state.clear()


app = FastAPI(
    title="Image Crops",
    description=(
        "YOLO OBB detection + rectified crop extraction, served as a warm "
        "HTTP microservice. Loads the model once at startup so requests skip "
        "the per-call model-load / MPS cold-start cost.\n\n"
        "Point `POST /tasks` at a server-local image path (or a directory of "
        "images, to process them all in one call) to get back detected boxes "
        "and (optionally) their saved, orientation-corrected crops. This "
        "service is detection+cropping only — no barcode decoding or OCR."
    ),
    lifespan=lifespan,
)


# Fields only ever populated by decode_crops()/ocr_failed_crops() in the
# decode_crops package (barcode decode + OCR orientation). This service never
# calls those, so on these entries they'd just be unpopulated placeholders.
_DECODE_ONLY_FIELDS = {
    "decoded",
    "decode_angle",
    "decode_method",
    "decode_attempts",
    "decode_failure_reason",
    "ocr",
    "ocr_angle",
    "orientation_confident",
    "orientation_margin",
    "orientation_source",
}


def _serializable_entry(entry: dict) -> dict:
    """`image`/`image_name` describe this box's own output crop file (its
    full saved path / basename) — each box gets its own crop, so that's the
    more useful per-box identifier than the shared source file (which is
    reported once, under the response's top-level `input`). `image`/
    `image_name` are None when crops aren't being saved (no output to point to)."""
    crop_path = entry.get("_crop_path")
    result = {
        key: value
        for key, value in entry.items()
        if key not in {"gray", "_crop_path", "image", "image_name"} | _DECODE_ONLY_FIELDS
    }
    result["image"] = crop_path
    result["image_name"] = Path(crop_path).name if crop_path else None
    return result


@app.get("/health")
def health():
    return {
        "status": "ok",
        "device": state.get("device"),
        "save_crops_dir": str(SAVE_CROPS_DIR) if SAVE_CROPS_DIR else None,
    }


@app.post("/tasks")
async def detect(
    image_path: str = Query(
        ...,
        description=(
            "Server-local path to an image file to detect+crop, or a directory of "
            "image files to process together in one call."
        ),
    ),
    workspace: str | None = Query(
        None,
        description=(
            "Root folder to write results into. Crops and output.json are written to "
            "'{workspace}/image_crops/'. If omitted, falls back to the server's "
            "SAVE_CROPS_DIR env var; if neither is set, crops aren't saved to disk."
        ),
    ),
    confidence: float = Query(
        0.25,
        description="YOLO detection confidence threshold (0-1). Boxes scoring below this are discarded.",
    ),
    pad_ratio: float = Query(
        0.15,
        description="Fractional margin added around each detected box before cropping (0.15 = 15% padding).",
    ),
    blur_threshold: float = Query(
        0.0,
        description=(
            "Minimum Laplacian-variance sharpness score required to process the image. "
            "The whole image is skipped (see skip_reason='blurry' in the response) if its "
            "score falls below this. 0 (default) disables the check."
        ),
    ),
):
    # Reads directly from server-local disk; caller is trusted to pass a
    # path the server should be allowed to read (same trust level as workspace).
    source_path = Path(image_path)
    is_batch = source_path.is_dir()

    if is_batch:
        source_paths = sorted(p for p in source_path.iterdir() if p.suffix.lower() in IMG_EXTS)
        if not source_paths:
            raise HTTPException(400, f"No image files found in image_path directory: {image_path!r}")
    else:
        if source_path.suffix.lower() not in IMG_EXTS:
            raise HTTPException(400, f"Unsupported file extension: {source_path.suffix!r}")
        if not source_path.is_file():
            raise HTTPException(400, f"image_path not found: {image_path!r}")
        source_paths = [source_path]

    # `workspace`, when given, wins over the server's fixed SAVE_CROPS_DIR.
    save_crops_dir = Path(workspace) / "image_crops" if workspace else SAVE_CROPS_DIR

    input_data = {
        "image_name": source_path.name,
        "image_path": image_path,
        "workspace": workspace,
        "confidence": confidence,
        "pad_ratio": pad_ratio,
        "blur_threshold": blur_threshold,
    }
    if is_batch:
        input_data["image_count"] = len(source_paths)

    start_at = datetime.now(timezone.utc)
    success = True
    error = None
    output_data: dict = {"n_skipped": 0, "crops_dir": str(save_crops_dir) if save_crops_dir else None}
    output_data.update({"images": []} if is_batch else {"boxes": [], "skip_reason": None})

    try:
        # detect_and_crop accepts multiple paths and loops over them internally
        # (one model.predict call per image, not a true batched inference).
        # `skipped` reports which specific images were skipped whole (never
        # detected on) and why — "unreadable" (cv2.imread failed) or "blurry".
        entries, skipped = detect_and_crop(
            state["model"],
            source_paths,
            conf=confidence,
            pad_ratio=pad_ratio,
            save_crops_dir=save_crops_dir,
            blur_thresh=blur_threshold,
            device=state["device"],
        )
        output_data["n_skipped"] = len(skipped)
        skipped_by_source = {s["image"]: s for s in skipped}

        if is_batch:
            # Group by source image before _serializable_entry overwrites
            # each entry's `image`/`image_name` with its own crop file.
            entries_by_source: dict[str, list] = {str(p): [] for p in source_paths}
            for entry in entries:
                entries_by_source[entry["image"]].append(entry)
            output_data["images"] = [
                {
                    "image_name": p.name,
                    "boxes": [_serializable_entry(e) for e in entries_by_source[str(p)]],
                    **{k: v for k, v in skipped_by_source.get(str(p), {}).items() if k not in {"image", "image_name", "reason"}},
                    "skip_reason": skipped_by_source.get(str(p), {}).get("reason"),
                }
                for p in source_paths
            ]
        else:
            output_data["boxes"] = [_serializable_entry(entry) for entry in entries]
            skip_info = skipped_by_source.get(str(source_path), {})
            output_data.update({k: v for k, v in skip_info.items() if k not in {"image", "image_name", "reason"}})
            output_data["skip_reason"] = skip_info.get("reason")
    except Exception as exc:
        success = False
        error = str(exc)

    finished_at = datetime.now(timezone.utc)

    result = {
        "input": input_data,
        "output": output_data,
        "start_at": start_at.isoformat(),
        "finished_at": finished_at.isoformat(),
        "success": success,
    }
    if error is not None:
        result["error"] = error

    if save_crops_dir:
        save_crops_dir.mkdir(parents=True, exist_ok=True)
        with open(save_crops_dir / "output.json", "w") as output_file:
            json.dump(result, output_file, indent=2)

    return result
