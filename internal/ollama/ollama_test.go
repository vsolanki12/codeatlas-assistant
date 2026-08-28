package ollama

import (
	"testing"
)

func TestUsageRecordsActualOllamaTokenCounts(t *testing.T) {
	client := &Client{Model: "test-model", NumCtx: 8192, MaxOutput: 256}
	client.setUsage("abcd", "hello world", 17, 2)
	usage := client.LastUsage()
	if usage.PromptTokens != 17 || usage.OutputTokens != 2 {
		t.Fatalf("actual Ollama counts were not recorded: %+v", usage)
	}
	if usage.Prompt.Characters != 4 || usage.Output.Characters != 11 {
		t.Fatalf("text measurements were not recorded: %+v", usage)
	}
}
