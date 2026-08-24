// Package main: PDF page-splitting logic — no HTTP concerns.
package main

import (
	"fmt"
	"os"
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
