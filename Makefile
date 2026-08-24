.PHONY: run-pipeline

# Starts every microservice (core, split_pdf, detect_boxes, crop_boxes,
# read_qr_ocr, gemini_ocr) locally and health-checks them. See
# scripts/run_pipeline.sh for the actual orchestration logic and its
# overridable env vars (ports, WORKSPACE_ROOT, RUN_GEMINI_OCR, ...).
run-pipeline:
	bash scripts/run_pipeline.sh
