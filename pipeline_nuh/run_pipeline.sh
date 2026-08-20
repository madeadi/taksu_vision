#!/usr/bin/env bash
# Runs the image_crops -> read_qr_ocr pipeline over every image in input_imgs/,
# writing results into this directory's image_crops/ and read_qr_ocr/ subfolders.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WORKSPACE="$SCRIPT_DIR"
INPUT_DIR="$WORKSPACE/input_imgs"

IMAGE_CROPS_DIR="$REPO_ROOT/image_crops"
READ_QR_OCR_DIR="$REPO_ROOT/read_qr_ocr"
GEMINI_OCR_DIR="$REPO_ROOT/gemini_ocr"

IMAGE_CROPS_PORT="${IMAGE_CROPS_PORT:-8818}"
READ_QR_OCR_PORT="${READ_QR_OCR_PORT:-8819}"
GEMINI_OCR_PORT="${GEMINI_OCR_PORT:-8820}"

IMAGE_CROPS_URL="http://127.0.0.1:${IMAGE_CROPS_PORT}"
READ_QR_OCR_URL="http://127.0.0.1:${READ_QR_OCR_PORT}"
GEMINI_OCR_URL="http://127.0.0.1:${GEMINI_OCR_PORT}"

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

echo "==> starting image_crops server on :$IMAGE_CROPS_PORT"
(
  cd "$IMAGE_CROPS_DIR"
  MODEL_PATH="$IMAGE_CROPS_DIR/weight.pt" \
    exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$IMAGE_CROPS_PORT" \
    >"$WORKSPACE/image_crops.log" 2>&1
) &
pids+=($!)
wait_for_health "$IMAGE_CROPS_URL" "image_crops"

echo "==> starting read_qr_ocr server on :$READ_QR_OCR_PORT"
(
  cd "$READ_QR_OCR_DIR"
  exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$READ_QR_OCR_PORT" \
    >"$WORKSPACE/read_qr_ocr.log" 2>&1
) &
pids+=($!)
wait_for_health "$READ_QR_OCR_URL" "read_qr_ocr"

if [[ "$RUN_GEMINI_OCR" == "true" ]]; then
  if [[ -z "${GEMINI_API_KEY:-}" && -z "${GOOGLE_API_KEY:-}" ]]; then
    echo "error: RUN_GEMINI_OCR=true but GEMINI_API_KEY (or GOOGLE_API_KEY) is not set" >&2
    exit 1
  fi
  echo "==> starting gemini_ocr server on :$GEMINI_OCR_PORT"
  (
    cd "$GEMINI_OCR_DIR"
    exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$GEMINI_OCR_PORT" \
      >"$WORKSPACE/gemini_ocr.log" 2>&1
  ) &
  pids+=($!)
  wait_for_health "$GEMINI_OCR_URL" "gemini_ocr"
fi

echo "==> servers up: image_crops ($IMAGE_CROPS_URL), read_qr_ocr ($READ_QR_OCR_URL)$([[ "$RUN_GEMINI_OCR" == "true" ]] && echo ", gemini_ocr ($GEMINI_OCR_URL)")"
wait
