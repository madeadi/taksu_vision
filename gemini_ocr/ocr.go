// Gemini-based OCR for already-cropped image files.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/genai"
)

var imgExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".bmp":  true,
	".webp": true,
}

var mimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".bmp":  "image/bmp",
	".webp": "image/webp",
}

const defaultInstruction = "Read all text visible in this image, exactly as it appears. " +
	"Include every piece of text you can see, in the order it appears, top to bottom and left to right."

type modelPrice struct {
	Input, Output float64 // USD per 1M tokens
}

// USD per 1M tokens, standard (non-batch) pricing as of 2026-08-21:
// https://ai.google.dev/gemini-api/docs/pricing
// gemini-2.5-pro's higher tier (>200k prompt tokens) isn't listed since a
// single cropped image never gets near that.
var modelPricing = map[string]modelPrice{
	"gemini-2.5-flash-lite": {Input: 0.10, Output: 0.40},
	"gemini-2.5-flash":      {Input: 0.30, Output: 2.50},
	"gemini-2.5-pro":        {Input: 1.25, Output: 10.00},
}

// costUSD returns the USD cost for one request, or (0, false) if model isn't
// in modelPricing.
func costUSD(model string, inputTokens, outputTokens int32) (float64, bool) {
	pricing, ok := modelPricing[model]
	if !ok {
		return 0, false
	}
	return (float64(inputTokens)*pricing.Input + float64(outputTokens)*pricing.Output) / 1_000_000, true
}

// OCRResult is one image's OCR outcome. A failure (bad image, API error,
// ...) is captured in Error instead of aborting the batch, so one bad image
// doesn't abort the rest.
type OCRResult struct {
	Image           string         `json:"image"`
	ImageName       string         `json:"image_name"`
	DetectedText    *string        `json:"detected_text"`
	MatchedPatterns []string       `json:"matched_patterns"`
	FormattedOutput map[string]any `json:"formatted_output"`
	InputTokens     *int32         `json:"input_tokens"`
	OutputTokens    *int32         `json:"output_tokens"`
	CostUSD         *float64       `json:"cost_usd"`
	Error           *string        `json:"error"`
}

// ocrModelReply is the shape ocrOne unmarshals the model's JSON reply into
// (enforced only by the prompt's closingInstruction, not a genai.Schema).
type ocrModelReply struct {
	DetectedText    string         `json:"detected_text"`
	MatchedPatterns []string       `json:"matched_patterns"`
	FormattedOutput map[string]any `json:"formatted_output"`
}

func ocrOne(ctx context.Context, client *genai.Client, imagePath, model, prompt string) OCRResult {
	result := OCRResult{Image: imagePath, ImageName: filepath.Base(imagePath)}

	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		errStr := err.Error()
		result.Error = &errStr
		return result
	}
	mimeType, ok := mimeTypes[strings.ToLower(filepath.Ext(imagePath))]
	if !ok {
		mimeType = "image/jpeg"
	}

	parts := []*genai.Part{
		genai.NewPartFromBytes(imageBytes, mimeType),
		genai.NewPartFromText(prompt),
	}
	content := []*genai.Content{genai.NewContentFromParts(parts, genai.RoleUser)}

	config := &genai.GenerateContentConfig{ResponseMIMEType: "application/json"}
	response, err := client.Models.GenerateContent(ctx, model, content, config)
	if err != nil {
		errStr := err.Error()
		result.Error = &errStr
		return result
	}

	var parsed ocrModelReply
	if jsonErr := json.Unmarshal([]byte(response.Text()), &parsed); jsonErr != nil {
		errStr := "invalid JSON response from model: " + jsonErr.Error()
		result.Error = &errStr
		// Fall through: the API call succeeded and consumed tokens even
		// though the reply was malformed, so still capture usage/cost below.
	} else {
		detectedText := parsed.DetectedText
		result.DetectedText = &detectedText
		result.MatchedPatterns = parsed.MatchedPatterns
		if result.MatchedPatterns == nil {
			result.MatchedPatterns = []string{}
		}
		result.FormattedOutput = parsed.FormattedOutput
		if result.FormattedOutput == nil {
			result.FormattedOutput = map[string]any{}
		}
	}

	if usage := response.UsageMetadata; usage != nil {
		inputTokens := usage.PromptTokenCount
		// Thinking tokens are billed as output alongside the visible response.
		outputTokens := usage.CandidatesTokenCount + usage.ThoughtsTokenCount
		result.InputTokens = &inputTokens
		result.OutputTokens = &outputTokens
		if cost, ok := costUSD(model, inputTokens, outputTokens); ok {
			result.CostUSD = &cost
		}
	}
	return result
}

// ocrImages OCRs each image in imagePaths with Gemini.
//
// Requests run in parallel, bounded by maxConcurrency (each call is a
// network-bound API request, so goroutines overlap I/O wait). Returns one
// OCRResult per input path, in the same order as imagePaths.
func ocrImages(ctx context.Context, client *genai.Client, imagePaths []string, model, prompt string, maxConcurrency int) []OCRResult {
	results := make([]OCRResult, len(imagePaths))
	if len(imagePaths) == 0 {
		return results
	}
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}

	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for i, path := range imagePaths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = ocrOne(ctx, client, path, model, prompt)
		}(i, path)
	}
	wg.Wait()
	return results
}
