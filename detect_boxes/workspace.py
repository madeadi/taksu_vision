"""Workspace-relative path resolution against a shared WORKSPACE_ROOT.

Every service in this repo that touches workspace files carries its own copy
of this module (no shared package between services — see README). Mirrors
`core/workspace.go`'s `resolveWorkspacePath` semantics: relative paths are
resolved against `{workspace_root}/{workspace_id}/files`, rejecting absolute
paths and anything that would escape that directory (directly or via a
symlink).
"""

from __future__ import annotations

import os
from pathlib import Path


def resolve_workspace_path(
    workspace_root: Path, workspace_id: str, relative_path: str, must_exist: bool = False
) -> Path:
    if os.path.isabs(relative_path):
        raise ValueError(f"relative path must not be absolute: {relative_path!r}")

    files_dir = workspace_root / workspace_id / "files"
    if not files_dir.is_dir():
        raise ValueError(f"workspace not found: {workspace_id!r}")

    real_base = files_dir.resolve(strict=True)
    candidate = (files_dir / relative_path).resolve(strict=False)
    if candidate != real_base and real_base not in candidate.parents:
        raise ValueError(f"relative path escapes workspace: {relative_path!r}")

    if must_exist and not candidate.exists():
        raise ValueError(f"path not found in workspace: {relative_path!r}")

    return candidate


def workspace_root_from_env() -> Path:
    value = os.environ.get("WORKSPACE_ROOT")
    if not value:
        raise RuntimeError("WORKSPACE_ROOT must be set")
    return Path(value)
