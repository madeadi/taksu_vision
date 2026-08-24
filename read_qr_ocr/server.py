"""FastAPI microservice: barcode decode (Data Matrix / Aztec) + OCR fallback
for crop files.

Takes already-detected, already-straightened crop image files as input
(e.g. the `crops_dir` written by ../image_crops's `POST /tasks`) — this
service does no detection/rectification of its own, just decoding. There's
no model to keep warm, so unlike image_crops/pipeline_nuh there's no
lifespan/startup load here.
"""

from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path

from fastapi import Body, FastAPI, HTTPException, Query

from decoder import OCR_ENGINES, decode_crops, load_crop_entries, ocr_failed_crops
from result import serializable_results
from workspace import resolve_workspace_path, workspace_root_from_env

IMG_EXTS = {".jpg", ".jpeg", ".png", ".bmp"}
WORKSPACE_ROOT = workspace_root_from_env()
# Default workspace-relative json_output_path used when a request omits it.
SAVE_RESULTS_DIR = os.environ.get("SAVE_RESULTS_DIR", "read_qr_ocr/output.json")

# Default is easyocr, not ocrmac: ocrmac calls into Apple's Vision framework
# (via PyObjC), which has been observed to SIGBUS-crash the whole process
# (native EXC_BAD_ACCESS inside ImageIO's EXIF parsing) on some inputs — a
# crash Python's exception handling can't catch or contain. easyocr is pure
# PyTorch, so a bad input there fails as a normal Python exception instead.
OCR_ENGINE = os.environ.get("OCR_ENGINE", "easyocr")
if OCR_ENGINE not in OCR_ENGINES:
    raise RuntimeError(f"OCR_ENGINE={OCR_ENGINE!r} is not one of {sorted(OCR_ENGINES)}")

app = FastAPI(
    title="Read QR OCR",
    description=(
        "Barcode decoding (Data Matrix / Aztec) for already-cropped label "
        "images, across a preprocessing ensemble (raw, CLAHE, upscaled, "
        "Otsu/adaptive threshold), with an optional OCR fallback "
        f"(`run_ocr=true`, engine configured via OCR_ENGINE — currently "
        f"'{OCR_ENGINE}') for crops the barcode decode can't read. Point "
        "`POST /tasks` at a server-local folder of crop files (e.g. from "
        "../image_crops) to get back each crop's decoded text."
    ),
)


@app.get("/health")
def health():
    return {
        "status": "ok",
        "save_results_dir": SAVE_RESULTS_DIR,
    }


@app.post("/tasks")
async def decode(
    workspace_id: str = Query(..., description="Workspace to read/write files in."),
    crops_dir: str = Query(
        ..., description="Directory (relative to the workspace) of already-cropped, already-straightened images to decode."
    ),
    filenames: list[str] | None = Body(
        None,
        embed=True,
        description=(
            "Optional JSON body {\"filenames\": [...]} of file names (within crops_dir) to "
            "restrict decoding to. If omitted, every image file in crops_dir is processed."
        ),
    ),
    json_output_path: str | None = Query(
        None,
        description=(
            "Path (relative to the workspace) to write the response JSON to. If omitted, "
            f"falls back to the server's SAVE_RESULTS_DIR env var (default '{SAVE_RESULTS_DIR}', "
            "also relative to the workspace)."
        ),
    ),
    upscale: float = Query(
        3.0, description="Upscale factor tried by the decode ensemble on difficult symbols."
    ),
    run_ocr: bool = Query(
        False,
        description=(
            "If true, run OCR (engine set by the server's OCR_ENGINE env var) on any "
            "crop that fails barcode decode, as a fallback. Off by default since it's "
            "slower than barcode decode alone (loads an OCR engine)."
        ),
    ),
    ocr_preprocess: bool = Query(
        True, description="When run_ocr is true: upscale + CLAHE-enhance each crop before OCR."
    ),
    ocr_scale: float = Query(
        3.0, description="When run_ocr and ocr_preprocess are true: the upscale factor applied before OCR."
    ),
):
    try:
        source_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, crops_dir, must_exist=True)
        workspace_files_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, "", must_exist=True)
        # `json_output_path`, when given, wins over the server's SAVE_RESULTS_DIR default.
        output_path = resolve_workspace_path(
            WORKSPACE_ROOT, workspace_id, json_output_path or SAVE_RESULTS_DIR
        )
    except ValueError as exc:
        raise HTTPException(400, str(exc))
    if not source_dir.is_dir():
        raise HTTPException(400, f"crops_dir not found or not a directory: {crops_dir!r}")

    if filenames:
        resolved_source = source_dir.resolve()
        crop_paths = []
        for name in filenames:
            candidate = (source_dir / name).resolve()
            # parent must be exactly source_dir: rejects path separators/traversal (e.g. "../x").
            if candidate.parent != resolved_source or not candidate.is_file():
                raise HTTPException(400, f"filenames entry not found in crops_dir: {name!r}")
            crop_paths.append(candidate)
    else:
        crop_paths = sorted(p for p in source_dir.iterdir() if p.suffix.lower() in IMG_EXTS)
    if not crop_paths:
        raise HTTPException(400, f"No image files found in crops_dir: {crops_dir!r}")

    input_data = {
        "crops_dir": crops_dir,
        "filenames": filenames,
        "json_output_path": json_output_path,
        "upscale": upscale,
        "run_ocr": run_ocr,
        "ocr_preprocess": ocr_preprocess,
        "ocr_scale": ocr_scale,
    }

    start_at = datetime.now(timezone.utc)
    success = True
    error = None
    output_data: dict = {"boxes": [], "n_decoded": 0, "n_ocr_read": 0}

    try:
        entries = load_crop_entries(crop_paths)
        decode_crops(entries, upscale_factor=upscale)
        if run_ocr:
            ocr_failed_crops(entries, engine=OCR_ENGINE, preprocess=ocr_preprocess, scale=ocr_scale)
        results = serializable_results(entries)
        for entry in results:
            if entry.get("image"):
                entry["image"] = str(Path(entry["image"]).relative_to(workspace_files_dir))
        output_data["boxes"] = results
        output_data["n_decoded"] = sum(1 for entry in entries if entry["decoded"])
        output_data["n_ocr_read"] = sum(1 for entry in entries if entry["ocr"])
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

    if output_path:
        output_path.parent.mkdir(parents=True, exist_ok=True)
        with open(output_path, "w") as output_file:
            json.dump(result, output_file, indent=2)

    return result
