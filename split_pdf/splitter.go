// Package main: PDF page-splitting logic — no HTTP concerns.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// PageResult is one output page from splitPDF.
type PageResult struct {
	PageNumber int    `json:"page_number"`
	PagePath   string `json:"page_path"`
}

// splitPDF splits pdfPath into single-page PDFs under pagesOutDir (created if
// missing), naming each file "{stem}_{page}.pdf" per pdfcpu's span-split
// convention (span=1). Returns one PageResult per page, in page order.
func splitPDF(pdfPath, pagesOutDir string) ([]PageResult, error) {
	if _, err := os.Stat(pdfPath); err != nil {
		return nil, fmt.Errorf("pdf_path not found: %s", pdfPath)
	}

	pageCount, err := api.PageCountFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read page count: %w", err)
	}

	if err := os.MkdirAll(pagesOutDir, 0o755); err != nil {
		return nil, fmt.Errorf("create pages_out_dir: %w", err)
	}

	if err := api.SplitFile(pdfPath, pagesOutDir, 1, nil); err != nil {
		return nil, fmt.Errorf("split pdf: %w", err)
	}

	stem := strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))
	pages := make([]PageResult, 0, pageCount)
	for page := 1; page <= pageCount; page++ {
		pages = append(pages, PageResult{
			PageNumber: page,
			PagePath:   filepath.Join(pagesOutDir, fmt.Sprintf("%s_%d.pdf", stem, page)),
		})
	}
	return pages, nil
}

// rasterizePDF renders pdfPath to one JPEG per page under pagesOutDir (created
// if missing), naming each file "{stem}_{page}.jpg". Shells out to Ghostscript
// (must be on PATH) to render every page in a single process invocation rather
// than one process per page. dpi controls render resolution.
func rasterizePDF(pdfPath, pagesOutDir string, dpi int) ([]PageResult, error) {
	if _, err := os.Stat(pdfPath); err != nil {
		return nil, fmt.Errorf("pdf_path not found: %s", pdfPath)
	}

	gsPath, err := exec.LookPath("gs")
	if err != nil {
		return nil, fmt.Errorf("ghostscript (gs) not found on PATH: %w", err)
	}

	pageCount, err := api.PageCountFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read page count: %w", err)
	}

	if err := os.MkdirAll(pagesOutDir, 0o755); err != nil {
		return nil, fmt.Errorf("create pages_out_dir: %w", err)
	}

	stem := strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))
	// Ghostscript's own "%d" page-number placeholder — one process renders
	// every page, rather than shelling out once per page.
	outputPattern := filepath.Join(pagesOutDir, stem+"_%d.jpg")

	cmd := exec.Command(gsPath,
		"-dNOPAUSE", "-dBATCH", "-dSAFER",
		"-sDEVICE=jpeg", "-dJPEGQ=90",
		fmt.Sprintf("-r%d", dpi),
		"-sOutputFile="+outputPattern,
		pdfPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ghostscript render failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	pages := make([]PageResult, 0, pageCount)
	for page := 1; page <= pageCount; page++ {
		pagePath := filepath.Join(pagesOutDir, fmt.Sprintf("%s_%d.jpg", stem, page))
		if _, err := os.Stat(pagePath); err != nil {
			return nil, fmt.Errorf("expected rendered page missing: %s: %s", pagePath, strings.TrimSpace(string(output)))
		}
		pages = append(pages, PageResult{PageNumber: page, PagePath: pagePath})
	}
	return pages, nil
}
