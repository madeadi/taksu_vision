"""FastAPI microservice: Gemini-based OCR for already-cropped images.

Takes already-detected, already-straightened crop image files as input
(e.g. the crops_dir written by ../image_crops's POST /tasks) and asks
Gemini to read the text out of each one. No detection/rectification of its
own — see ../image_crops for that. There's no model to keep warm (the
Gemini client just wraps HTTP calls to Google's API), so unlike
image_crops/pipeline_nuh there's no lifespan/startup load here.
"""

from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path

from fastapi import Body, FastAPI, HTTPException, Query
from google import genai

from ocr import DEFAULT_PROMPT, IMG_EXTS, ocr_images
from workspace import resolve_workspace_path, workspace_root_from_env

API_KEY = os.environ.get("GEMINI_API_KEY") or os.environ.get("GOOGLE_API_KEY")
if not API_KEY:
    raise RuntimeError("GEMINI_API_KEY (or GOOGLE_API_KEY) must be set")

DEFAULT_MODEL = os.environ.get("GEMINI_MODEL", "gemini-2.5-flash-lite")
DEFAULT_MAX_CONCURRENCY = int(os.environ.get("MAX_CONCURRENCY", "4"))
WORKSPACE_ROOT = workspace_root_from_env()
# Default workspace-relative json_output_path used when a request omits it.
SAVE_RESULTS_DIR = os.environ.get("SAVE_RESULTS_DIR", "gemini_ocr/output.json")

client = genai.Client(api_key=API_KEY)

app = FastAPI(
    title="Gemini OCR",
    description=(
        "OCR for already-cropped images via the Gemini API "
        f"(default model '{DEFAULT_MODEL}', override with the `model` query "
        "param or the GEMINI_MODEL env var). Point `POST /tasks` at a "
        "server-local folder of crop files (e.g. from ../image_crops) to "
        "get back each image's path and recognized text."
    ),
)


@app.get("/health")
def health():
    return {
        "status": "ok",
        "model": DEFAULT_MODEL,
        "save_results_dir": SAVE_RESULTS_DIR,
    }


@app.post("/tasks")
async def ocr(
    workspace_id: str = Query(..., description="Workspace to read/write files in."),
    images_dir: str = Query(
        ..., description="Directory (relative to the workspace) of already-cropped images to OCR."
    ),
    filenames: list[str] | None = Body(
        None,
        embed=True,
        description=(
            "Optional JSON body {\"filenames\": [...]} of file names (within images_dir) to "
            "restrict OCR to. If omitted, every image file in images_dir is processed."
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
    model: str = Query(DEFAULT_MODEL, description="Gemini model to OCR with."),
    prompt: str = Query(DEFAULT_PROMPT, description="Prompt sent alongside each image."),
    max_concurrency: int = Query(
        DEFAULT_MAX_CONCURRENCY, description="Max number of Gemini requests to run in parallel."
    ),
):
    try:
        source_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, images_dir, must_exist=True)
        workspace_files_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, "", must_exist=True)
        # `json_output_path`, when given, wins over the server's SAVE_RESULTS_DIR default.
        output_path = resolve_workspace_path(
            WORKSPACE_ROOT, workspace_id, json_output_path or SAVE_RESULTS_DIR
        )
    except ValueError as exc:
        raise HTTPException(400, str(exc))
    if not source_dir.is_dir():
        raise HTTPException(400, f"images_dir not found or not a directory: {images_dir!r}")

    if filenames:
        resolved_source = source_dir.resolve()
        image_paths = []
        for name in filenames:
            candidate = (source_dir / name).resolve()
            # parent must be exactly source_dir: rejects path separators/traversal (e.g. "../x").
            if candidate.parent != resolved_source or not candidate.is_file():
                raise HTTPException(400, f"filenames entry not found in images_dir: {name!r}")
            image_paths.append(candidate)
    else:
        image_paths = sorted(p for p in source_dir.iterdir() if p.suffix.lower() in IMG_EXTS)
    if not image_paths:
        raise HTTPException(400, f"No image files found in images_dir: {images_dir!r}")

    input_data = {
        "images_dir": images_dir,
        "filenames": filenames,
        "json_output_path": json_output_path,
        "model": model,
        "prompt": prompt,
        "max_concurrency": max_concurrency,
    }

    start_at = datetime.now(timezone.utc)
    success = True
    error = None
    output_data: dict = {"results": [], "n_processed": 0, "n_failed": 0}

    try:
        results = ocr_images(
            client, image_paths, model=model, prompt=prompt, max_concurrency=max_concurrency
        )
        for entry in results:
            if entry.get("image"):
                entry["image"] = str(Path(entry["image"]).relative_to(workspace_files_dir))
        output_data["results"] = results
        output_data["n_processed"] = sum(1 for r in results if r["error"] is None)
        output_data["n_failed"] = sum(1 for r in results if r["error"] is not None)
        output_data["total_input_tokens"] = sum(r["input_tokens"] or 0 for r in results)
        output_data["total_output_tokens"] = sum(r["output_tokens"] or 0 for r in results)
        # Sums costs of images that have one; None only if none do (e.g. every
        # image failed, or `model` isn't in ocr.MODEL_PRICING).
        known_costs = [r["cost_usd"] for r in results if r["cost_usd"] is not None]
        output_data["total_cost_usd"] = sum(known_costs) if known_costs else None
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
