"""Dataset assembly and job bookkeeping for /train.

Training itself runs in a child process (`train_job.py`) so the FastAPI
event loop — and therefore `/health` — stays responsive for the whole run.
This module only assembles the YOLO-OBB dataset from a labels JSON file,
spawns that child process, and reads back its status.
"""

from __future__ import annotations

import json
import os
import random
import subprocess
import sys
import uuid
from datetime import datetime, timezone
from pathlib import Path

import cv2

# Matches the polygon convention `detector.py` emits: 4 [x, y] pixel corners.
Polygon = list[list[float]]


def normalize_polygon(polygon: Polygon, img_w: int, img_h: int) -> list[float]:
    """Flatten a pixel-space polygon into normalized xyxyxyxy, clipped to [0, 1]."""
    flat = []
    for x, y in polygon:
        flat.append(min(max(x / img_w, 0.0), 1.0))
        flat.append(min(max(y / img_h, 0.0), 1.0))
    return flat


def assemble_dataset(
    job_dir: Path,
    images_dir: Path,
    labels: dict[str, list[dict]],
    class_names: list[str],
    val_split: float,
    seed: int,
) -> Path:
    """Build a YOLO-OBB dataset under `job_dir/dataset` and return its data.yaml path.

    `labels` maps image file name -> list of `{"polygon": [[x, y]] * 4, "class": int}`
    (`class` optional, defaults to 0). Images are symlinked in (same disk as the
    workspace); labels are written as normalized YOLO-OBB txt files. Images are
    split train/val by `val_split`, shuffled deterministically by `seed`.
    """
    dataset_dir = job_dir / "dataset"
    names = sorted(labels.keys())
    if not names:
        raise ValueError("labels file has no entries")

    rng = random.Random(seed)
    rng.shuffle(names)
    n_val = max(1, round(len(names) * val_split)) if len(names) > 1 else 0
    val_names = set(names[:n_val])

    for split in ("train", "val"):
        (dataset_dir / "images" / split).mkdir(parents=True, exist_ok=True)
        (dataset_dir / "labels" / split).mkdir(parents=True, exist_ok=True)

    for image_name in names:
        split = "val" if image_name in val_names else "train"
        src = images_dir / image_name
        if not src.is_file():
            raise ValueError(f"labels reference missing image: {image_name!r}")

        image = cv2.imread(str(src))
        if image is None:
            raise ValueError(f"could not read image: {image_name!r}")
        img_h, img_w = image.shape[:2]

        image_link = dataset_dir / "images" / split / image_name
        if not image_link.exists():
            os.symlink(src.resolve(), image_link)

        lines = []
        for box in labels[image_name]:
            polygon = box["polygon"]
            class_id = int(box.get("class", 0))
            coords = normalize_polygon(polygon, img_w, img_h)
            lines.append(" ".join([str(class_id), *(f"{c:.6f}" for c in coords)]))

        label_path = dataset_dir / "labels" / split / f"{Path(image_name).stem}.txt"
        label_path.write_text("\n".join(lines) + ("\n" if lines else ""))

    data_yaml = dataset_dir / "data.yaml"
    data_yaml.write_text(
        "path: {}\ntrain: images/train\nval: images/val\nnc: {}\nnames: {}\n".format(
            dataset_dir.resolve(), len(class_names), json.dumps(class_names)
        )
    )
    return data_yaml


def start_job(
    detect_boxes_dir: Path,
    job_dir: Path,
    data_yaml: Path,
    base_weights: Path,
    weights_out: Path,
    epochs: int,
    imgsz: int,
    batch: int,
    patience: int,
    device: str,
) -> str:
    """Spawn the training subprocess and return its job id (== job_dir.name)."""
    job_dir.mkdir(parents=True, exist_ok=True)
    log_path = job_dir / "train.log"
    status_path = job_dir / "status.json"
    status_path.write_text(
        json.dumps({"status": "pending", "start_at": datetime.now(timezone.utc).isoformat()})
    )

    with open(log_path, "w") as log_file:
        subprocess.Popen(
            [
                sys.executable,
                str(detect_boxes_dir / "train_job.py"),
                "--job-dir", str(job_dir),
                "--data", str(data_yaml),
                "--base-weights", str(base_weights),
                "--weights-out", str(weights_out),
                "--epochs", str(epochs),
                "--imgsz", str(imgsz),
                "--batch", str(batch),
                "--patience", str(patience),
                "--device", device,
            ],
            cwd=str(detect_boxes_dir),
            stdout=log_file,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
    return job_dir.name


def read_job_status(job_dir: Path) -> dict:
    """Read back a job's status.json, detecting a dead process the child never
    got to report (e.g. killed -9) and marking it failed."""
    status_path = job_dir / "status.json"
    if not status_path.is_file():
        raise ValueError(f"unknown job: {job_dir.name!r}")

    status = json.loads(status_path.read_text())
    if status.get("status") in ("pending", "running"):
        pid = status.get("pid")
        if pid is not None and not _pid_alive(pid):
            status["status"] = "failed"
            status["error"] = "training process is no longer running"
            status["finished_at"] = datetime.now(timezone.utc).isoformat()
    return status


def list_jobs(train_root: Path) -> list[dict]:
    """Read back every job's status.json under `train_root` (a workspace's
    `_train` directory), newest first by `start_at`. Returns `[]` if
    `train_root` doesn't exist yet (no jobs started). Skips any job
    directory `read_job_status` can't parse rather than failing the whole
    listing."""
    if not train_root.is_dir():
        return []

    jobs = []
    for job_dir in train_root.iterdir():
        if not job_dir.is_dir():
            continue
        try:
            status = read_job_status(job_dir)
        except ValueError:
            continue
        jobs.append({"job_id": job_dir.name, **status})

    jobs.sort(key=lambda job: job.get("start_at") or "", reverse=True)
    return jobs


def _pid_alive(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def new_job_id() -> str:
    return uuid.uuid4().hex


def infer_class_names(labels: dict[str, list[dict]]) -> list[str]:
    """Fall back to generic names sized to the highest class index seen in `labels`."""
    max_class = -1
    for boxes in labels.values():
        for box in boxes:
            max_class = max(max_class, int(box.get("class", 0)))
    return [f"class_{i}" for i in range(max_class + 1)] or ["object"]
