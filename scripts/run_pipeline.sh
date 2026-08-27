#!/usr/bin/env bash
# Starts core, split_pdf, detect_boxes, crop_boxes, read_qr_ocr, and (unless
# RUN_GEMINI_OCR=false) gemini_ocr. Each is a standalone HTTP server; this
# script only starts them and health-checks them (no per-image
# orchestration) — callers hit their /tasks (or, for core, CRUD) endpoints
# directly.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Runtime dir for this script's own output (logs, default workspace root) —
# gitignored, not to be confused with a service's WORKSPACE_ROOT.
WORKSPACE="$REPO_ROOT/.run"
mkdir -p "$WORKSPACE"
INPUT_DIR="$WORKSPACE/input_imgs"

# Shared workspace root: every service below reads/writes workspace files
# directly under here (workspace_id + relative path), so this must be set
# identically on every service process. `core` owns creating/uploading
# into/downloading from/deleting the workspaces that live here.
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$WORKSPACE/workspaces}"
mkdir -p "$WORKSPACE_ROOT"
export WORKSPACE_ROOT

CORE_DIR="$REPO_ROOT/core"
SPLIT_PDF_DIR="$REPO_ROOT/split_pdf"
DETECT_BOXES_DIR="$REPO_ROOT/detect_boxes"
CROP_BOXES_DIR="$REPO_ROOT/crop_boxes"
READ_QR_OCR_DIR="$REPO_ROOT/read_qr_ocr"
GEMINI_OCR_DIR="$REPO_ROOT/gemini_ocr"

CORE_PORT="${CORE_PORT:-8824}"
SPLIT_PDF_PORT="${SPLIT_PDF_PORT:-8823}"
READ_QR_OCR_PORT="${READ_QR_OCR_PORT:-8819}"
GEMINI_OCR_PORT="${GEMINI_OCR_PORT:-8820}"
DETECT_BOXES_PORT="${DETECT_BOXES_PORT:-8821}"
CROP_BOXES_PORT="${CROP_BOXES_PORT:-8822}"

CORE_URL="http://127.0.0.1:${CORE_PORT}"
SPLIT_PDF_URL="http://127.0.0.1:${SPLIT_PDF_PORT}"
READ_QR_OCR_URL="http://127.0.0.1:${READ_QR_OCR_PORT}"
GEMINI_OCR_URL="http://127.0.0.1:${GEMINI_OCR_PORT}"
DETECT_BOXES_URL="http://127.0.0.1:${DETECT_BOXES_PORT}"
CROP_BOXES_URL="http://127.0.0.1:${CROP_BOXES_PORT}"

CONFIDENCE="${CONFIDENCE:-0.25}"
PAD_RATIO="${PAD_RATIO:-0.15}"
UPSCALE="${UPSCALE:-3.0}"
RUN_OCR="${RUN_OCR:-true}"
RUN_GEMINI_OCR="${RUN_GEMINI_OCR:-true}"

pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

wait_for_health() {
  local url="$1" name="$2"
  for _ in $(seq 1 60); do
    if curl -s -o /dev/null -f "$url/health"; then
      return 0
    fi
    sleep 1
  done
  echo "error: $name did not become healthy at $url" >&2
  exit 1
}

# Reads a top-level "key: value" scalar out of a simple YAML file (good
# enough for gemini_ocr/config.yaml's flat shape; not a general YAML parser).
# Strips surrounding quotes. Prints nothing if the file or key is absent.
yaml_get() {
  local file="$1" key="$2"
  [[ -f "$file" ]] || return 0
  awk -F': *' -v k="$key" '$1==k {v=$2; gsub(/^"|"$/, "", v); print v; exit}' "$file"
}

echo "==> starting core server on :$CORE_PORT"
(
  cd "$CORE_DIR"
  exec go run . --host 127.0.0.1 --port "$CORE_PORT" \
    >"$WORKSPACE/core.log" 2>&1
) &
pids+=($!)
wait_for_health "$CORE_URL" "core"

echo "==> starting split_pdf server on :$SPLIT_PDF_PORT"
(
  cd "$SPLIT_PDF_DIR"
  exec go run . --host 127.0.0.1 --port "$SPLIT_PDF_PORT" \
    >"$WORKSPACE/split_pdf.log" 2>&1
) &
pids+=($!)
wait_for_health "$SPLIT_PDF_URL" "split_pdf"

echo "==> starting detect_boxes server on :$DETECT_BOXES_PORT"
(
  cd "$DETECT_BOXES_DIR"
  MODEL_PATH="$DETECT_BOXES_DIR/weight.pt" \
    exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$DETECT_BOXES_PORT" \
    >"$WORKSPACE/detect_boxes.log" 2>&1
) &
pids+=($!)
wait_for_health "$DETECT_BOXES_URL" "detect_boxes"

echo "==> starting crop_boxes server on :$CROP_BOXES_PORT"
(
  cd "$CROP_BOXES_DIR"
  exec go run . --host 127.0.0.1 --port "$CROP_BOXES_PORT" \
    >"$WORKSPACE/crop_boxes.log" 2>&1
) &
pids+=($!)
wait_for_health "$CROP_BOXES_URL" "crop_boxes"

echo "==> starting read_qr_ocr server on :$READ_QR_OCR_PORT"
(
  cd "$READ_QR_OCR_DIR"
  exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$READ_QR_OCR_PORT" \
    >"$WORKSPACE/read_qr_ocr.log" 2>&1
) &
pids+=($!)
wait_for_health "$READ_QR_OCR_URL" "read_qr_ocr"

if [[ "$RUN_GEMINI_OCR" == "true" ]]; then
  # gemini_ocr also accepts config.yaml (see gemini_ocr/README.md's
  # Configuration section); config.yaml values win over env vars there, so
  # check it too instead of just the env vars this script itself sets.
  GEMINI_OCR_CONFIG="$GEMINI_OCR_DIR/config.yaml"
  GEMINI_OCR_CONFIG_API_KEY="$(yaml_get "$GEMINI_OCR_CONFIG" gemini_api_key)"
  if [[ -z "${GEMINI_API_KEY:-}" && -z "${GOOGLE_API_KEY:-}" && -z "$GEMINI_OCR_CONFIG_API_KEY" ]]; then
    echo "error: RUN_GEMINI_OCR=true but GEMINI_API_KEY (or GOOGLE_API_KEY) is not set, and $GEMINI_OCR_CONFIG has no gemini_api_key" >&2
    exit 1
  fi

  # config.yaml's workspace_root, if set, overrides this script's
  # WORKSPACE_ROOT for gemini_ocr specifically — every other service uses
  # $WORKSPACE_ROOT directly, so a mismatch here would silently split
  # gemini_ocr onto a different workspace disk than the rest of the pipeline.
  GEMINI_OCR_CONFIG_WORKSPACE_ROOT="$(yaml_get "$GEMINI_OCR_CONFIG" workspace_root)"
  if [[ -n "$GEMINI_OCR_CONFIG_WORKSPACE_ROOT" && "$GEMINI_OCR_CONFIG_WORKSPACE_ROOT" != "$WORKSPACE_ROOT" ]]; then
    echo "error: $GEMINI_OCR_CONFIG's workspace_root ($GEMINI_OCR_CONFIG_WORKSPACE_ROOT) does not match WORKSPACE_ROOT ($WORKSPACE_ROOT) used by every other service" >&2
    exit 1
  fi

  echo "==> starting gemini_ocr server on :$GEMINI_OCR_PORT"
  (
    cd "$GEMINI_OCR_DIR"
    exec go run . --host 127.0.0.1 --port "$GEMINI_OCR_PORT" \
      >"$WORKSPACE/gemini_ocr.log" 2>&1
  ) &
  pids+=($!)
  wait_for_health "$GEMINI_OCR_URL" "gemini_ocr"
fi

echo "==> servers up: core ($CORE_URL), split_pdf ($SPLIT_PDF_URL), detect_boxes ($DETECT_BOXES_URL), crop_boxes ($CROP_BOXES_URL), read_qr_ocr ($READ_QR_OCR_URL)$([[ "$RUN_GEMINI_OCR" == "true" ]] && echo ", gemini_ocr ($GEMINI_OCR_URL)")"
echo "==> WORKSPACE_ROOT: $WORKSPACE_ROOT"
wait
