"""JSON-backed registry of trained model weights.

Global to this service (not scoped per workspace, unlike `weights_path` in
`server.py`'s `/tasks` and `/train`): one `weights.json` file in this
directory. Entries are added automatically when a `/train` job succeeds (see
`train_job.py`) and store an absolute filesystem path, since a workspace-
relative path wouldn't be enough to locate the file outside that workspace.

Reads/writes are guarded with an flock since more than one training
subprocess can finish and register a weight around the same time.
"""

from __future__ import annotations

import fcntl
import json
from pathlib import Path
from typing import Callable, TypeVar

REGISTRY_PATH = Path(__file__).parent / "weights.json"

T = TypeVar("T")


def _with_lock(registry_path: Path, fn: Callable[[list[dict]], T]) -> T:
    registry_path.touch(exist_ok=True)
    with open(registry_path, "r+") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        try:
            raw = f.read()
            entries = json.loads(raw) if raw.strip() else []
            result = fn(entries)
            f.seek(0)
            f.truncate()
            json.dump(entries, f, indent=2)
            return result
        finally:
            fcntl.flock(f, fcntl.LOCK_UN)


def list_weights(registry_path: Path = REGISTRY_PATH) -> list[dict]:
    if not registry_path.is_file():
        return []
    raw = registry_path.read_text()
    return json.loads(raw) if raw.strip() else []


def register_weight(
    name: str, description: str, path: str, registry_path: Path = REGISTRY_PATH
) -> dict:
    """Add (or replace, if `name` already exists) a weight entry."""
    entry = {"name": name, "description": description, "path": path}

    def _add(entries: list[dict]) -> dict:
        entries[:] = [e for e in entries if e["name"] != name]
        entries.append(entry)
        return entry

    return _with_lock(registry_path, _add)


def delete_weight(name: str, registry_path: Path = REGISTRY_PATH) -> bool:
    """Remove a weight entry by name. Returns whether an entry was removed.

    Only removes the registry entry — the underlying weights file on disk is
    left in place.
    """

    def _remove(entries: list[dict]) -> bool:
        before = len(entries)
        entries[:] = [e for e in entries if e["name"] != name]
        return len(entries) < before

    return _with_lock(registry_path, _remove)
