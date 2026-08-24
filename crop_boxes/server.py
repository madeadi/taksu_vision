"""FastAPI microservice: crop extraction from pre-detected boxes.

Stateless — no model to keep warm, so there's no lifespan/startup step. Feed
it the boxes from `../detect_boxes`'s response (or any source producing the
same shape) and it returns padded, orientation-corrected crop files.
"""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

from fastapi import FastAPI, HTTPException, Query
from pydantic import BaseModel

from cropper import crop_boxes
from workspace import resolve_workspace_path, workspace_root_from_env

WORKSPACE_ROOT = workspace_root_from_env()

app = FastAPI(
    title="Crop Boxes",
    description=(
        "Crop extraction from pre-detected boxes, served as a stateless HTTP "
        "microservice — no YOLO model, just image geometry. Feed it a "
        "workspace-relative image path plus the boxes from `../detect_boxes`'s "
        "response and it returns padded, orientation-corrected crop files, "
        "written under the same workspace. Crop-only — no detection, no "
        "barcode decoding, no OCR."
    ),
)


class Box(BaseModel):
    box_index: int
    is_obb: bool = False
    xyxy: list[float] | None = None
    polygon: list[list[float]] | None = None


@app.get("/health")
def health():
    return {"status": "ok"}


@app.post("/tasks")
async def run_crop(
    boxes: list[Box],
    workspace_id: str = Query(..., description="Workspace to read/write files in."),
    image_path: str = Query(
        ...,
        description="Path (relative to the workspace) of the image to crop from.",
    ),
    pad_ratio: float = Query(
        0.15,
        description="Fractional margin added around each box before cropping (0.15 = 15% padding).",
    ),
    crops_out_dir: str = Query(
        ...,
        description="Directory (relative to the workspace) to write crop files into. Created if it doesn't exist.",
    ),
    json_output_path: str | None = Query(
        None,
        description="Path (relative to the workspace) to write the response JSON to. If omitted, the response isn't written to disk.",
    ),
):
    try:
        source_path = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, image_path, must_exist=True)
        crops_out_dir_path = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, crops_out_dir)
        json_output_path_obj = (
            resolve_workspace_path(WORKSPACE_ROOT, workspace_id, json_output_path)
            if json_output_path
            else None
        )
    except ValueError as exc:
        raise HTTPException(400, str(exc))
    if not source_path.is_file():
        raise HTTPException(400, f"image_path not found: {image_path!r}")

    for box in boxes:
        if box.is_obb and box.polygon is None:
            raise HTTPException(400, f"box_index {box.box_index}: is_obb=true requires polygon")
        if not box.is_obb and box.xyxy is None:
            raise HTTPException(400, f"box_index {box.box_index}: is_obb=false requires xyxy")

    # Resolved (symlinks followed) the same way resolve_workspace_path()
    # resolved crops_out_dir_path above, so relative_to() below compares
    # paths from the same realm (matters if WORKSPACE_ROOT sits behind a
    # symlink, e.g. macOS's /tmp -> /private/tmp).
    workspace_files_dir = resolve_workspace_path(WORKSPACE_ROOT, workspace_id, "", must_exist=True)

    input_data = {
        "image_name": source_path.name,
        "image_path": image_path,
        "pad_ratio": pad_ratio,
        "crops_out_dir": crops_out_dir,
        "json_output_path": json_output_path,
        "box_count": len(boxes),
    }

    start_at = datetime.now(timezone.utc)
    success = True
    error = None
    output_data: dict = {"crops": [], "crops_dir": crops_out_dir}

    try:
        entries = crop_boxes(
            source_path,
            [box.model_dump() for box in boxes],
            pad_ratio=pad_ratio,
            save_crops_dir=crops_out_dir_path,
        )
        for entry in entries:
            if entry["crop_path"] is not None:
                entry["crop_path"] = str(Path(entry["crop_path"]).relative_to(workspace_files_dir))
        output_data["crops"] = entries
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
