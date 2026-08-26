"""Subprocess entrypoint for a single training run.

Spawned by `train.start_job`, one process per job. Writes `status.json` in
`--job-dir` at start (status=running, pid) and at the end (status=succeeded
or failed). Runs standalone — no imports from `server.py` — so a crash here
can't take the API server down with it.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import traceback
from datetime import datetime, timezone
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--job-dir", required=True)
    parser.add_argument("--data", required=True)
    parser.add_argument("--base-weights", required=True)
    parser.add_argument("--weights-out", required=True)
    parser.add_argument("--epochs", type=int, required=True)
    parser.add_argument("--imgsz", type=int, required=True)
    parser.add_argument("--batch", type=int, required=True)
    parser.add_argument("--patience", type=int, required=True)
    parser.add_argument("--device", required=True)
    return parser.parse_args()


def write_status(status_path: Path, **fields) -> None:
    current = {}
    if status_path.is_file():
        current = json.loads(status_path.read_text())
    current.update(fields)
    status_path.write_text(json.dumps(current, indent=2))


def main() -> None:
    args = parse_args()
    job_dir = Path(args.job_dir)
    status_path = job_dir / "status.json"

    write_status(
        status_path,
        status="running",
        pid=os.getpid(),
        start_at=datetime.now(timezone.utc).isoformat(),
    )

    try:
        from ultralytics import YOLO

        model = YOLO(args.base_weights)
        results = model.train(
            data=args.data,
            epochs=args.epochs,
            imgsz=args.imgsz,
            batch=args.batch,
            patience=args.patience,
            device=args.device,
            project=str(job_dir / "runs"),
            name="train",
            exist_ok=True,
            verbose=True,
        )

        best_weights = Path(results.save_dir) / "weights" / "best.pt"
        weights_out = Path(args.weights_out)
        weights_out.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(best_weights, weights_out)

        metrics = {
            key: round(float(value), 4)
            for key, value in results.results_dict.items()
        }

        write_status(
            status_path,
            status="succeeded",
            finished_at=datetime.now(timezone.utc).isoformat(),
            metrics=metrics,
            weights_out=str(weights_out),
        )

        try:
            import weights as weight_registry

            map50 = metrics.get("metrics/mAP50(B)") or metrics.get("metrics/mAP50(OBB)")
            description = f"{args.epochs} epochs from {Path(args.base_weights).name}"
            if map50 is not None:
                description += f", mAP50={map50}"
            weight_registry.register_weight(job_dir.name, description, str(weights_out))
        except Exception:
            # Registry bookkeeping must not flip a succeeded job to failed.
            traceback.print_exc()
    except Exception:
        write_status(
            status_path,
            status="failed",
            finished_at=datetime.now(timezone.utc).isoformat(),
            error=traceback.format_exc(),
        )
        raise


if __name__ == "__main__":
    main()
