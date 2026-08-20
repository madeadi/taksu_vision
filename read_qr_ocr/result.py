"""Compatibility serialization for the existing pipeline JSON contract."""

from __future__ import annotations

from entry import CropEntry


def serializable_results(entries: list[CropEntry]) -> list[dict]:
    """Remove in-memory image arrays while retaining JSON-safe result fields."""
    return [
        {
            key: value
            for key, value in entry.items()
            if key not in {"gray", "_crop_path"}
        }
        for entry in entries
    ]
