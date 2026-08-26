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
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles
from ultralytics import YOLO

import train
import weights as weight_registry
from detector import IMG_EXTS, detect
from workspace import resolve_workspace_path, workspace_root_from_env

DETECT_BOXES_DIR = Path(__file__).parent
WEB_DIST_DIR = DETECT_BOXES_DIR / "web" / "dist"

MODEL_PATH = os.environ.get("MODEL_PATH", "weight.pt")
DEVICE_ENV = os.environ.get("DEVICE")  # unset -> auto (mps on Apple Silicon, else cpu)
WORKSPACE_ROOT = workspace_root_from_env()

# TODO: revisit once request auth (e.g. JWT) is in front of this API — an
# open CORS policy is fine while every endpoint is unauthenticated local dev
# tooling, but not once these routes can act on a caller's behalf.
DEFAULT_CORS_ALLOWED_ORIGINS = "*"


def cors_allowed_origins() -> list[str]:
    """CORS_ALLOWED_ORIGINS as a comma-separated list of origins allowed to
    call this API cross-origin (e.g. the workspace management UI's dev/prod
    origin). Defaults to the Vite dev server origin."""
    v = os.environ.get("CORS_ALLOWED_ORIGINS", DEFAULT_CORS_ALLOWED_ORIGINS)
    return [origin.strip() for origin in v.split(",") if origin.strip()]

state: dict = {}


def resolve_device(explicit: str | None) -> str:
    if explicit:
        return explicit
    import torch

    return "mps" if torch.backends.mps.is_available() else "cpu"


def get_model(weights_path: Path | None):
    """Return the model to detect with: the cached load for `weights_path`,
    or this server's startup model (`state["model"]`) when omitted."""
    if weights_path is None:
        return state["model"]

    key = str(weights_path)
    cache = state.setdefault("model_cache", {})
    if key not in cache:
        print(f"Loading model {key}")
        cache[key] = YOLO(key)
    return cache[key]


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

app.add_middleware(
    CORSMiddleware,
    allow_origins=cors_allowed_origins(),
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
def health():
    return {"status": "ok", "device": state.get("device")}


@app.get("/weights")
def list_weights():
    return {"weights": weight_registry.list_weights()}


@app.delete("/weights/{name}")
def delete_weight(name: str):
    if not weight_registry.delete_weight(name):
        raise HTTPException(404, f"weight not found: {name!r}")
    return {"deleted": name}


@app.post("/tasks")
async def run_detect(
    workspace_id: str = Query(..., description="Workspace to read/write files in."),
    images_dir: str = Query(
        "",
        description="Directory (relative to the workspace) of image files to detect. Empty = workspace root.",
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
            "Path (relative to the workspace) to write the response JSON to. If omitted, the "
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
    weights_path: str | None = Query(
        None,
        description=(
            "Weights file (relative to the workspace) to detect with, instead of this "
            "server's startup model (MODEL_PATH). Loaded on first use and cached for reuse "
            "across requests."
        ),
    ),
):
    try:
        source_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, images_dir, must_exist=True)
        json_output_path_obj = (
            resolve_workspace_path(WORKSPACE_ROOT, workspace_id, json_output_path)
            if json_output_path
            else None
        )
        weights_path_obj = (
            resolve_workspace_path(WORKSPACE_ROOT, workspace_id, weights_path, must_exist=True)
            if weights_path
            else None
        )
    except ValueError as exc:
        raise HTTPException(400, str(exc))
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
        "weights_path": weights_path,
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
            get_model(weights_path_obj),
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

    if json_output_path_obj:
        json_output_path_obj.parent.mkdir(parents=True, exist_ok=True)
        with open(json_output_path_obj, "w") as output_file:
            json.dump(result, output_file, indent=2)

    return result


@app.post("/train")
async def start_train(
    workspace_id: str = Query(..., description="Workspace to read/write files in."),
    images_dir: str = Query(..., description="Directory (relative to the workspace) of the raw training images."),
    labels_path: str = Query(
        ...,
        description=(
            "JSON file (relative to the workspace) mapping each image file name to its "
            "box list: {\"img.jpg\": [{\"polygon\": [[x,y]]*4, \"class\": 0}, ...]}. "
            "\"class\" is optional (defaults to 0). This is exactly the shape of the "
            "\"polygon\" field POST /tasks returns per box, so /tasks output — reviewed "
            "and corrected — can be fed straight back in as labels."
        ),
    ),
    weights_out_path: str = Query(
        ..., description="Path (relative to the workspace) to write the trained best.pt to."
    ),
    base_weights_path: str | None = Query(
        None,
        description=(
            "Checkpoint to start training from (relative to the workspace). Omit to warm-start "
            "from this server's currently loaded model (MODEL_PATH)."
        ),
    ),
    class_names: list[str] | None = Body(
        None,
        embed=True,
        description="Class names in index order. Omit to infer generic names from the class indices in labels_path.",
    ),
    epochs: int = Query(150, description="Training epochs."),
    imgsz: int = Query(1024, description="Training image size."),
    batch: int = Query(4, description="Training batch size."),
    patience: int = Query(30, description="Epochs with no improvement before early stopping."),
    val_split: float = Query(0.2, description="Fraction of images held out for validation."),
):
    try:
        source_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, images_dir, must_exist=True)
        labels_file = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, labels_path, must_exist=True)
        weights_out = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, weights_out_path)
        base_weights = (
            resolve_workspace_path(WORKSPACE_ROOT, workspace_id, base_weights_path, must_exist=True)
            if base_weights_path
            else Path(MODEL_PATH).resolve()
        )
    except ValueError as exc:
        raise HTTPException(400, str(exc))
    if not source_dir.is_dir():
        raise HTTPException(400, f"images_dir not found or not a directory: {images_dir!r}")

    try:
        labels = json.loads(labels_file.read_text())
    except json.JSONDecodeError as exc:
        raise HTTPException(400, f"labels_path is not valid JSON: {exc}")
    if not isinstance(labels, dict):
        raise HTTPException(400, "labels_path must be a JSON object of {image_name: [box, ...]}")

    input_data = {
        "images_dir": images_dir,
        "labels_path": labels_path,
        "weights_out_path": weights_out_path,
        "base_weights_path": base_weights_path,
        "class_names": class_names,
        "epochs": epochs,
        "imgsz": imgsz,
        "batch": batch,
        "patience": patience,
        "val_split": val_split,
    }
    start_at = datetime.now(timezone.utc)

    try:
        job_id = train.new_job_id()
        job_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, f"_train/{job_id}")
        resolved_class_names = class_names or train.infer_class_names(labels)
        data_yaml = train.assemble_dataset(
            job_dir, source_dir, labels, resolved_class_names, val_split, seed=int(job_id[:8], 16)
        )
        train.start_job(
            DETECT_BOXES_DIR,
            job_dir,
            data_yaml,
            base_weights,
            weights_out,
            epochs=epochs,
            imgsz=imgsz,
            batch=batch,
            patience=patience,
            device=state["device"],
        )
    except ValueError as exc:
        raise HTTPException(400, str(exc))

    return {
        "input": input_data,
        "output": {"job_id": job_id, "status": "pending", "class_names": resolved_class_names},
        "start_at": start_at.isoformat(),
        "finished_at": None,
        "success": True,
    }


@app.get("/train")
async def list_train_jobs(
    workspace_id: str = Query(..., description="Workspace to list training jobs in."),
):
    try:
        train_root = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, "_train")
    except ValueError as exc:
        raise HTTPException(400, str(exc))

    return {
        "input": {"workspace_id": workspace_id},
        "output": {"jobs": train.list_jobs(train_root)},
        "success": True,
    }


@app.get("/train/{job_id}")
async def get_train_status(
    job_id: str,
    workspace_id: str = Query(..., description="Workspace the job was started in."),
):
    try:
        job_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, f"_train/{job_id}", must_exist=True)
        status = train.read_job_status(job_dir)
    except ValueError as exc:
        raise HTTPException(404, str(exc))

    result = {
        "input": {"workspace_id": workspace_id, "job_id": job_id},
        "output": {k: v for k, v in status.items() if k not in {"error"}},
        "start_at": status.get("start_at"),
        "finished_at": status.get("finished_at"),
        "success": status.get("status") != "failed",
    }
    if status.get("status") == "failed":
        result["error"] = status.get("error")
    return result


# Serves the built web app (see README.md) at /web on this same
# port/process, once it's been built (`cd web && npm run build` -> web/dist;
# vite.config.ts sets base: "/web/" for that build so its asset URLs match
# this mount point). Registered last so it only catches paths no route above
# matched — the API routes and /docs/openapi.json above always take
# priority. Absent in dev, where the web app instead runs its own
# `npm run dev` server (see web/).
if WEB_DIST_DIR.is_dir():
    app.mount("/web", StaticFiles(directory=str(WEB_DIST_DIR), html=True), name="web")
