// Package metrics contains small, dependency-free measurements used to report
// context size without pretending that character counts are exact tokenizer
// counts.
package metrics

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// Text describes a text payload. EstimatedTokens is a stable approximation
// intended for comparing prompts, not billing or model-token accounting.
type Text struct {
	Bytes           int `json:"bytes"`
	Characters      int `json:"characters"`
	EstimatedTokens int `json:"estimatedTokens"`
}

// Measure returns byte, Unicode character, and approximate token counts.
// Four characters per token is deliberately conservative for source text.
func Measure(value string) Text {
	characters := utf8.RuneCountInString(value)
	estimatedTokens := 0
	if characters > 0 {
		estimatedTokens = (characters + 3) / 4
	}
	return Text{
		Bytes:           len(value),
		Characters:      characters,
		EstimatedTokens: estimatedTokens,
	}
}

// ReductionPercent reports how much smaller after is than before.
func ReductionPercent(before, after int) float64 {
	if before <= 0 {
		return 0
	}
	value := (float64(before-after) / float64(before)) * 100
	return math.Round(value*10) / 10
}

// FormatText is intended for concise stderr/CLI reports.
func FormatText(value Text) string {
	return fmt.Sprintf("%d bytes, %d chars, ~%d estimated tokens", value.Bytes, value.Characters, value.EstimatedTokens)
}
