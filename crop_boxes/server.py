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

app = FastAPI(
    title="Crop Boxes",
    description=(
        "Crop extraction from pre-detected boxes, served as a stateless HTTP "
        "microservice — no YOLO model, just image geometry. Feed it a "
        "server-local image path plus the boxes from `../detect_boxes`'s "
        "response and it returns padded, orientation-corrected crop files. "
        "Crop-only — no detection, no barcode decoding, no OCR."
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
    image_path: str = Query(
        ...,
        description="Server-local path to the image to crop from (no file upload).",
    ),
    pad_ratio: float = Query(
        0.15,
        description="Fractional margin added around each box before cropping (0.15 = 15% padding).",
    ),
    crops_out_dir: str = Query(
        ...,
        description="Directory to write crop files into. Created if it doesn't exist.",
    ),
    json_output_path: str | None = Query(
        None,
        description="Full path to write the response JSON to. If omitted, the response isn't written to disk.",
    ),
):
    # Reads directly from server-local disk; caller is trusted to pass a
    # path the server should be allowed to read (same trust level as crops_out_dir).
    source_path = Path(image_path)
    if not source_path.is_file():
        raise HTTPException(400, f"image_path not found: {image_path!r}")

    for box in boxes:
        if box.is_obb and box.polygon is None:
            raise HTTPException(400, f"box_index {box.box_index}: is_obb=true requires polygon")
        if not box.is_obb and box.xyxy is None:
            raise HTTPException(400, f"box_index {box.box_index}: is_obb=false requires xyxy")

    crops_out_dir_path = Path(crops_out_dir)

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
    output_data: dict = {"crops": [], "crops_dir": str(crops_out_dir_path)}

    try:
        entries = crop_boxes(
            image_path,
            [box.model_dump() for box in boxes],
            pad_ratio=pad_ratio,
            save_crops_dir=crops_out_dir_path,
        )
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

    if json_output_path:
        json_output_path_obj = Path(json_output_path)
        json_output_path_obj.parent.mkdir(parents=True, exist_ok=True)
        with open(json_output_path_obj, "w") as output_file:
            json.dump(result, output_file, indent=2)

    return result
