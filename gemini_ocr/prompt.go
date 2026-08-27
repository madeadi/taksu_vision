// Combines the four caller-supplied extraction inputs (instruction,
// formatted_output example, patterns, remove_patterns) into one prompt
// string, applied identically to every image in a /tasks batch.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExtractionConfig holds the caller-supplied inputs that get combined into
// one extraction prompt for a /tasks batch. Instruction is assumed already
// resolved to a non-empty value by the caller (same convention as
// model/max_concurrency defaulting in tasksHandler).
type ExtractionConfig struct {
	Instruction     string
	FormattedOutput map[string]any
	Patterns        []string
	RemovePatterns  []string
}

// closingInstruction is fixed and NOT user-configurable: it's what makes
// ocrOne's json.Unmarshal of the model's reply reliable.
const closingInstruction = `Reply with ONLY a single JSON object. No commentary, no markdown code fences, no explanation before or after it. The JSON object must have exactly these three keys:
- "detected_text": string. The specific answer to the instruction above — NOT a transcript of every piece of text visible in the image. (If the instruction is simply to read all visible text, then that full transcript IS the answer.) After finding it, remove any of the remove_patterns strings listed below (if any were given). This filtered answer is also what gets checked against "patterns". Empty string if the instruction's answer isn't present in the image.
- "matched_patterns": array of strings. The exact strings, copied verbatim, from the "patterns" list below that describe something present in "detected_text". Empty array if no patterns were given, or none matched.
- "formatted_output": if a formatted_output example is given below, an object with that same shape/keys, populated with values read from the image (use a blank string for any field you can't find). If NO formatted_output example is given below, this MUST be exactly {} — never invent keys or values of your own.
If a value can't be determined, use a blank/empty value for it (empty string, empty array, empty object) — never omit the key, and never refuse to answer.`

// buildPrompt combines cfg into one prompt string sent alongside each image.
func buildPrompt(cfg ExtractionConfig) string {
	var b strings.Builder

	b.WriteString(cfg.Instruction)
	b.WriteString("\n\n")

	if len(cfg.FormattedOutput) > 0 {
		example, err := json.MarshalIndent(cfg.FormattedOutput, "", "  ")
		if err == nil {
			b.WriteString("formatted_output example (populate an object with this shape and these keys; " +
				"the sample values below are illustrative only, replace them with what you read):\n")
			b.Write(example)
			b.WriteString("\n\n")
		}
	}

	if len(cfg.Patterns) > 0 {
		b.WriteString("patterns — check detected_text against each of these, and echo back verbatim (in matched_patterns) any that match:\n")
		for _, p := range cfg.Patterns {
			fmt.Fprintf(&b, "- %s\n", p)
		}
		b.WriteString("\n")
	}

	if len(cfg.RemovePatterns) > 0 {
		b.WriteString("remove_patterns — strip any text matching these out of detected_text BEFORE checking it against patterns above:\n")
		for _, p := range cfg.RemovePatterns {
			fmt.Fprintf(&b, "- %s\n", p)
		}
		b.WriteString("\n")
	}

	b.WriteString(closingInstruction)
	return b.String()
}
