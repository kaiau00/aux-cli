package models

import "testing"

// The name shown in the task header and status bar is derived from the model
// ID. It may be shortened for display, but it must never silently drop the
// part that distinguishes one model from another: a user running MiniMax-M3
// saw "MiniMax M", which is indistinguishable from M1 or M2.
func TestFriendlyModelNameKeepsTheDistinguishingParts(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"MiniMax-M3", "MiniMax M3"},
		{"minimax-m2", "Minimax M2"},
		{"kimi-k2", "Kimi K2"},
		{"deepseek-r1", "Deepseek R1"},
		{"glm-4.6", "Glm 4.6"},
		{"phi-4", "Phi 4"},
		{"qwen3-coder", "Qwen3 Coder"},
		{"gpt-oss-120b", "Gpt Oss 120b"},
		{"llama-3.3-70b", "Llama 3.3 70b"},
		{"codestral-22b", "Codestral 22b"},
		{"Qwen2.5-Coder-32B-Instruct", "Qwen2.5 Coder 32B Instruct"},
		{"devstral-small-2507", "Devstral Small 2507"},
		{"mistral-nemo", "Mistral Nemo"},
		// A provider path prefix is noise; the tag after @ is not.
		{"lmstudio-community/MiniMax-M3", "MiniMax M3"},
		{"llama3.1:8b@q4", "Llama3.1:8b q4"},
	} {
		if got := friendlyModelName(tc.id); got != tc.want {
			t.Errorf("friendlyModelName(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// Whatever the shortening rules are, every alphanumeric run in the ID has to
// survive into the display name. This is the property the table above samples.
func TestFriendlyModelNameNeverDropsAnAlphanumericRun(t *testing.T) {
	for _, id := range []string{
		"MiniMax-M3", "kimi-k2", "Qwen2.5-Coder-32B-Instruct", "llama-3.3-70b",
		"gpt-oss-120b", "gemma-3-27b-it", "claude-sonnet-4-5", "codestral-22b",
	} {
		got := friendlyModelName(id)
		for _, run := range splitAlnumRuns(id) {
			if !containsFold(got, run) {
				t.Errorf("friendlyModelName(%q) = %q, dropped %q", id, got, run)
			}
		}
	}
}

func splitAlnumRuns(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '-' || r == '_' || r == '/' || r == '@' || r == '.' || r == ':' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func containsFold(haystack, needle string) bool {
	h, n := []rune(lower(haystack)), []rune(lower(needle))
	for i := 0; i+len(n) <= len(h); i++ {
		if string(h[i:i+len(n)]) == string(n) {
			return true
		}
	}
	return false
}

func lower(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		out = append(out, r)
	}
	return string(out)
}
