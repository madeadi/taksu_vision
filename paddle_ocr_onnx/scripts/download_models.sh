#!/usr/bin/env bash
# Download PP-OCRv4 (det + rec) and angle-classifier (cls) ONNX models and the character dictionary into public/models/.
# The ONNX files are PaddleOCR's official PP-OCRv4 mobile models, pre-converted with paddle2onnx
# and hosted by the RapidOCR project.
set -euo pipefail
cd "$(dirname "$0")/../public/models"
HF=https://huggingface.co/SWHL/RapidOCR/resolve/main/PP-OCRv4
[[ -s det.onnx ]] || curl -fL --progress-bar -o det.onnx "$HF/ch_PP-OCRv4_det_infer.onnx"
[[ -s rec.onnx ]] || curl -fL --progress-bar -o rec.onnx "$HF/ch_PP-OCRv4_rec_infer.onnx"
[[ -s cls.onnx ]] || curl -fL --progress-bar -o cls.onnx "https://huggingface.co/SWHL/RapidOCR/resolve/main/PP-OCRv1/ch_ppocr_mobile_v2.0_cls_infer.onnx"
[[ -s keys.txt ]] || curl -fL --progress-bar -o keys.txt \
  https://raw.githubusercontent.com/PaddlePaddle/PaddleOCR/release/2.7/ppocr/utils/ppocr_keys_v1.txt
ls -lh
