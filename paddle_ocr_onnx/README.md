# paddle_ocr_onnx

Browser-only demo of PaddleOCR **PP-OCRv4** (DBNet text detector + SVTR/CTC
recognizer) running on [`onnxruntime-web`](https://onnxruntime.ai/) (WebAssembly).
Pick, drop or paste an image and it detects and reads text locally, drawing the
detected boxes over the image. Sibling of `../zxing_wasm`: a static app with no
backend, and images never leave the browser.

## Run

```bash
npm install
npm run models   # downloads det.onnx / cls.onnx / rec.onnx / keys.txt into public/models (~17 MB, git-ignored)
npm run dev      # http://localhost:8831
npm run build    # static output in dist/ (relative asset paths, host anywhere, works offline)
```

Models are the official PaddleOCR PP-OCRv4 mobile weights, pre-converted to ONNX
by the RapidOCR project (see `scripts/download_models.sh`). To convert your own,
use `paddle2onnx` and drop the files in `public/models/` as `det.onnx`, `rec.onnx`.

## Layout

- `src/ocr.ts`: model loading, OpenCV-based box extraction and cropping, angle classification, CTC decode.
- `src/App.tsx`: the demo UI.

## How it handles rotation

Detection post-processing uses [`@techstark/opencv-js`](https://www.npmjs.com/package/@techstark/opencv-js)
(lazy-loaded, ~13 MB, only fetched when the app starts):
`findContours` -> `minAreaRect` -> DB unclip -> `warpPerspective` to a flat crop. Vertical crops
(h/w >= 1.5) are turned 90 degrees, then the PP-OCR angle classifier (`cls.onnx`) flips upside-down
lines 180 degrees before recognition. So tilted, vertical and upside-down text all work; a
whole image rotated 90 degrees is handled by the vertical-crop rule per line.

## Limitations

- Curved text is approximated by a single rotated rectangle.
- The Chinese+English v4 recognizer often drops spaces between words
  (`SerialNo` for `Serial No`).
- WASM runs single-threaded unless the page is cross-origin isolated
  (COOP/COEP headers). Expect roughly 0.5-2 s per image.
