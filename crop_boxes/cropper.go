// Crop geometry for axis-aligned and oriented boxes — no YOLO dependency.
// Ported 1:1 from cropper.py; see that file's docstrings for the original
// rationale (kept here as comments where the "why" isn't obvious from code).
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// Box mirrors detect_boxes's POST /tasks per-box output shape.
type Box struct {
	BoxIndex int         `json:"box_index"`
	IsOBB    bool        `json:"is_obb"`
	XYXY     []float64   `json:"xyxy,omitempty"`
	Polygon  [][]float64 `json:"polygon,omitempty"`
}

// CropEntry is one crop_boxes result entry.
type CropEntry struct {
	BoxIndex     int     `json:"box_index"`
	IsOBB        bool    `json:"is_obb"`
	CropPath     *string `json:"crop_path"`
	LayoutAngle  int     `json:"layout_angle"`
	LayoutMargin float64 `json:"layout_margin"`
}

// orderQuadPoints orders a convex quadrilateral as top-left, top-right,
// bottom-right, bottom-left.
func orderQuadPoints(quad [4]point2) [4]point2 {
	var center point2
	for _, p := range quad {
		center.X += p.X / 4
		center.Y += p.Y / 4
	}
	type indexed struct {
		p     point2
		angle float64
	}
	items := make([]indexed, 4)
	for i, p := range quad {
		items[i] = indexed{p, math.Atan2(p.Y-center.Y, p.X-center.X)}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].angle < items[j].angle })

	topLeftIndex := 0
	minSum := math.Inf(1)
	for i, it := range items {
		s := it.p.X + it.p.Y
		if s < minSum {
			minSum = s
			topLeftIndex = i
		}
	}

	var ordered [4]point2
	for i := 0; i < 4; i++ {
		ordered[i] = items[(topLeftIndex+i)%4].p
	}
	return ordered
}

// expandQuad expands a quadrilateral around its center and keeps it inside
// the image.
func expandQuad(quad [4]point2, padRatio float64, width, height int) [4]point2 {
	var center point2
	for _, p := range quad {
		center.X += p.X / 4
		center.Y += p.Y / 4
	}
	scale := 1.0 + 2.0*padRatio
	var out [4]point2
	for i, p := range quad {
		x := center.X + (p.X-center.X)*scale
		y := center.Y + (p.Y-center.Y)*scale
		out[i] = point2{
			X: clampFloat(x, 0, math.Max(0, float64(width-1))),
			Y: clampFloat(y, 0, math.Max(0, float64(height-1))),
		}
	}
	return out
}

// axisAlignedCrop crops an axis-aligned xyxy box with proportional padding.
func axisAlignedCrop(image *Mat, xyxy []float64, padRatio float64) *Mat {
	x1, y1, x2, y2 := xyxy[0], xyxy[1], xyxy[2], xyxy[3]
	boxWidth, boxHeight := x2-x1, y2-y1
	padX, padY := boxWidth*padRatio, boxHeight*padRatio
	left := maxInt(0, int(x1-padX))
	top := maxInt(0, int(y1-padY))
	right := minInt(image.Width, int(x2+padX))
	bottom := minInt(image.Height, int(y2+padY))
	if right <= left || bottom <= top {
		return newMat(0, 0)
	}
	return subImage(image, left, top, right, bottom)
}

func subImage(m *Mat, x0, y0, x1, y1 int) *Mat {
	w, h := x1-x0, y1-y0
	out := newMat(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := m.at(x0+x, y0+y)
			out.set(x, y, r, g, b)
		}
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func norm(a, b point2) float64 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// rectifyOBBCrop perspective-warps an OBB into a canonical crop with its
// long side horizontal.
func rectifyOBBCrop(image *Mat, points [4]point2, padRatio float64) *Mat {
	ordered := orderQuadPoints(expandQuad(points, padRatio, image.Width, image.Height))
	topLeft, topRight, bottomRight, bottomLeft := ordered[0], ordered[1], ordered[2], ordered[3]

	outputWidth := int(math.Round(math.Max(norm(topRight, topLeft), norm(bottomRight, bottomLeft))))
	outputHeight := int(math.Round(math.Max(norm(bottomLeft, topLeft), norm(bottomRight, topRight))))
	if outputWidth < 2 || outputHeight < 2 {
		return newMat(0, 0)
	}

	destination := [4]point2{
		{0, 0},
		{float64(outputWidth - 1), 0},
		{float64(outputWidth - 1), float64(outputHeight - 1)},
		{0, float64(outputHeight - 1)},
	}
	transform := perspectiveTransform(ordered, destination)
	crop := warpPerspective(image, transform, outputWidth, outputHeight)

	if crop.Height > crop.Width {
		crop = rotate90CW(crop)
	}
	return crop
}

// normalizeLabelDirection puts the dense Data Matrix end of a horizontal
// label on the left.
//
// OBB geometry can normalize the long axis, but it cannot distinguish 0°
// from 180°. These labels have a stable layout: the Data Matrix is the
// highest-detail square at the left end and the human-readable text follows.
func normalizeLabelDirection(crop *Mat) (*Mat, int, float64) {
	if crop.Width == 0 || crop.Height == 0 || crop.Width <= crop.Height {
		return crop, 0, 0.0
	}

	gray := grayscale(crop)
	height, width := crop.Height, crop.Width
	bandWidth := maxInt(height, minInt(width/3, int(math.Round(float64(height)*1.35))))

	leftScore := sobelMagnitudeMean(gray, bandWidth, height, 0, width)
	rightScore := sobelMagnitudeMean(gray, bandWidth, height, width-bandWidth, width)
	denominator := math.Max(math.Max(leftScore, rightScore), 1.0)
	margin := math.Abs(leftScore-rightScore) / denominator

	// Avoid changing ambiguous/partial crops where neither end clearly
	// contains the Data Matrix. OCR remains available for those cases.
	if rightScore > leftScore && margin >= 0.08 {
		return rotate180(crop), 180, margin
	}
	return crop, 0, margin
}

// CropBoxes crops boxes out of the image at imagePath.
//
// Each item in boxes has BoxIndex, IsOBB, and either XYXY (plain boxes) or
// Polygon (OBB boxes, as 4 [x, y] corners — any ordering, they're re-ordered
// internally). This is exactly the shape ../detect_boxes's POST /tasks
// returns per box, so its output.boxes (or output.images[i].boxes) can be
// passed straight through.
//
// OBB boxes are perspective-rectified via rectifyOBBCrop and
// orientation-normalized via normalizeLabelDirection; plain boxes are
// padded-and-cropped via axisAlignedCrop with no rotation/orientation logic,
// since there's no angle info.
//
// saveCropsDir, if non-empty, is where crop files are written (created if
// missing); crop_path in the result is empty if saveCropsDir is empty. Boxes
// producing an empty crop (e.g. a degenerate/too-small OBB) are skipped
// entirely.
func CropBoxes(imagePath string, boxes []Box, padRatio float64, saveCropsDir string) ([]CropEntry, error) {
	image, err := loadMat(imagePath)
	if err != nil {
		return nil, fmt.Errorf("could not read image: %s", imagePath)
	}
	if saveCropsDir != "" {
		if err := os.MkdirAll(saveCropsDir, 0o755); err != nil {
			return nil, fmt.Errorf("create save_crops_dir: %w", err)
		}
	}

	entries := make([]CropEntry, 0, len(boxes))
	stem := stemOf(imagePath)
	for _, box := range boxes {
		var crop *Mat
		var layoutAngle int
		var layoutMargin float64

		if box.IsOBB {
			var points [4]point2
			for i, p := range box.Polygon {
				points[i] = point2{p[0], p[1]}
			}
			crop = rectifyOBBCrop(image, points, padRatio)
			crop, layoutAngle, layoutMargin = normalizeLabelDirection(crop)
		} else {
			crop = axisAlignedCrop(image, box.XYXY, padRatio)
			layoutAngle, layoutMargin = 0, 0.0
		}

		if crop.Width == 0 || crop.Height == 0 {
			continue
		}

		var cropPath *string
		if saveCropsDir != "" {
			name := fmt.Sprintf("%s_box%d.jpg", stem, box.BoxIndex)
			path := filepath.Join(saveCropsDir, name)
			if err := saveJPEG(path, crop, 85); err != nil {
				return nil, fmt.Errorf("save crop: %w", err)
			}
			cropPath = &path
		}

		entries = append(entries, CropEntry{
			BoxIndex:     box.BoxIndex,
			IsOBB:        box.IsOBB,
			CropPath:     cropPath,
			LayoutAngle:  layoutAngle,
			LayoutMargin: math.Round(layoutMargin*1000) / 1000,
		})
	}
	return entries, nil
}

func stemOf(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return base[:len(base)-len(ext)]
}
