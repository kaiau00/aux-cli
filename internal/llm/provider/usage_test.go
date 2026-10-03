package provider

import (
	"testing"

	"github.com/openai/openai-go"
)

func usageWith(prompt, cached, completion int64) openai.CompletionUsage {
	u := openai.CompletionUsage{PromptTokens: prompt, CompletionTokens: completion}
	u.PromptTokensDetails.CachedTokens = cached
	return u
}

// The bug this guards: openai-go's streaming accumulator drops
// prompt_tokens_details through v1.12.0, so a response whose prefix was almost entirely cached
// was recorded as entirely fresh input -- overstating cost and making the cost
// governor stop work against a number that was too high.
func TestStreamUsagePrefersTheChunkCarryingCacheDetails(t *testing.T) {
	chunk := usageWith(2179, 2178, 8)    // what the wire reports
	accumulator := usageWith(2179, 0, 8) // what the accumulator loses

	got := streamUsage(chunk, accumulator)
	if got.PromptTokensDetails.CachedTokens != 2178 {
		t.Fatalf("cached tokens %d, want 2178: the chunk's details must win over the accumulator's",
			got.PromptTokensDetails.CachedTokens)
	}
}

// A provider that only populates the accumulator must keep working.
func TestStreamUsageFallsBackToTheAccumulator(t *testing.T) {
	got := streamUsage(openai.CompletionUsage{}, usageWith(500, 100, 20))
	if got.PromptTokens != 500 || got.PromptTokensDetails.CachedTokens != 100 {
		t.Fatalf("expected the accumulator's usage when no chunk carried any, got %+v", got)
	}
}

// Cached tokens are billed separately, so they must not also be counted as
// fresh input or the same tokens are paid for twice.
func TestUsageSplitsCachedTokensOutOfInput(t *testing.T) {
	c := &openaiClient{}
	got := c.usage(usageWith(2179, 2178, 8))

	if got.InputTokens != 1 {
		t.Fatalf("input tokens %d, want 1 (2179 prompt - 2178 cached)", got.InputTokens)
	}
	if got.CacheReadTokens != 2178 {
		t.Fatalf("cache read tokens %d, want 2178", got.CacheReadTokens)
	}
	if got.OutputTokens != 8 {
		t.Fatalf("output tokens %d, want 8", got.OutputTokens)
	}
}

func TestUsageWithNoCacheIsUnchanged(t *testing.T) {
	c := &openaiClient{}
	got := c.usage(usageWith(1000, 0, 50))
	if got.InputTokens != 1000 || got.CacheReadTokens != 0 {
		t.Fatalf("an uncached response should report all prompt tokens as input, got %+v", got)
	}
}
