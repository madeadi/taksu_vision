"""FastAPI microservice: YOLO bounding-box detection only (no cropping).

Loads the YOLO model once at startup so it stays warm across requests,
instead of paying model-load / MPS cold-start cost on every call.
"""

from __future__ import annotations

import json
import os
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from pathlib import Path

from fastapi import Body, FastAPI, HTTPException, Query
from ultralytics import YOLO

from detector import IMG_EXTS, detect

MODEL_PATH = os.environ.get("MODEL_PATH", "weight.pt")
DEVICE_ENV = os.environ.get("DEVICE")  # unset -> auto (mps on Apple Silicon, else cpu)

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
    title="Detect Boxes",
    description=(
        "YOLO bounding-box detection, served as a warm HTTP microservice. "
        "Loads the model once at startup so requests skip the per-call "
        "model-load / MPS cold-start cost.\n\n"
        "Point `POST /tasks` at a server-local directory of images to get "
        "back each image's detected boxes — axis-aligned `xyxy` plus the "
        "ordered 4-corner `polygon` (oriented for OBB models, the box's own "
        "corners otherwise). Detection only — no cropping (see "
        "`../crop_boxes`), no barcode decoding, no OCR."
    ),
    lifespan=lifespan,
)


@app.get("/health")
def health():
    return {"status": "ok", "device": state.get("device")}


@app.post("/tasks")
async def run_detect(
    images_dir: str = Query(
        ...,
        description="Server-local directory of image files to detect.",
    ),
    filenames: list[str] | None = Body(
        None,
        embed=True,
        description=(
            "Optional JSON body {\"filenames\": [...]} of file names (within images_dir) to "
            "restrict detection to. If omitted, every image file in images_dir is processed."
        ),
    ),
    json_output_path: str | None = Query(
        None,
        description=(
            "Full path to write the response JSON to. If omitted, the "
            "response isn't written to disk — only returned in the HTTP response."
        ),
    ),
    confidence: float = Query(
        0.25,
        description="YOLO detection confidence threshold (0-1). Boxes scoring below this are discarded.",
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
    # path the server should be allowed to read (same trust level as json_output_path).
    source_dir = Path(images_dir)
    if not source_dir.is_dir():
        raise HTTPException(400, f"images_dir not found or not a directory: {images_dir!r}")

    if filenames:
        resolved_source = source_dir.resolve()
        source_paths = []
        for name in filenames:
            candidate = (source_dir / name).resolve()
            # parent must be exactly source_dir: rejects path separators/traversal (e.g. "../x").
            if candidate.parent != resolved_source or not candidate.is_file():
                raise HTTPException(400, f"filenames entry not found in images_dir: {name!r}")
            source_paths.append(candidate)
    else:
        source_paths = sorted(p for p in source_dir.iterdir() if p.suffix.lower() in IMG_EXTS)
    if not source_paths:
        raise HTTPException(400, f"No image files found in images_dir: {images_dir!r}")

    input_data = {
        "images_dir": images_dir,
        "filenames": filenames,
        "image_count": len(source_paths),
        "json_output_path": json_output_path,
        "confidence": confidence,
        "blur_threshold": blur_threshold,
    }

    start_at = datetime.now(timezone.utc)
    success = True
    error = None
    output_data: dict = {"n_skipped": 0, "images": []}

    try:
        # detect accepts multiple paths and loops over them internally (one
        # model.predict call per image, not a true batched inference).
        # `skipped` reports which specific images were skipped whole (never
        # detected on) and why — "unreadable" (cv2.imread failed) or "blurry".
        entries, skipped = detect(
            state["model"],
            source_paths,
            conf=confidence,
            blur_thresh=blur_threshold,
            device=state["device"],
        )
        output_data["n_skipped"] = len(skipped)
        skipped_by_source = {s["image"]: s for s in skipped}

        def _box_fields(entry: dict) -> dict:
            return {k: v for k, v in entry.items() if k not in {"image", "image_name"}}

        entries_by_source: dict[str, list] = {str(p): [] for p in source_paths}
        for entry in entries:
            entries_by_source[entry["image"]].append(entry)
        output_data["images"] = [
            {
                "image_name": p.name,
                "boxes": [_box_fields(e) for e in entries_by_source[str(p)]],
                **{k: v for k, v in skipped_by_source.get(str(p), {}).items() if k not in {"image", "image_name", "reason"}},
                "skip_reason": skipped_by_source.get(str(p), {}).get("reason"),
            }
            for p in source_paths
        ]
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

    if json_output_path:
        json_output_path_obj = Path(json_output_path)
        json_output_path_obj.parent.mkdir(parents=True, exist_ok=True)
        with open(json_output_path_obj, "w") as output_file:
            json.dump(result, output_file, indent=2)

    return result
