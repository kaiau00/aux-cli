package prompt

import (
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
)

// PonytailProtocol and project-context files should only be appended to the
// agents that actually write code. Title and Summarizer prompts must stay
// unopinionated; injecting YAGNI dogma into a title model pollutes session
// titles. We test the predicate directly to avoid the package-level
// sync.Once that getContextFromPaths uses, which would otherwise be shared
// with other tests in this package.
func TestAgentUsesPonytail(t *testing.T) {
	cases := []struct {
		agent config.AgentName
		want  bool
	}{
		{config.AgentCoder, true},
		{config.AgentTask, true},
		{config.AgentTitle, false},
		{config.AgentSummarizer, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.agent), func(t *testing.T) {
			if got := agentUsesPonytail(tc.agent); got != tc.want {
				t.Fatalf("agentUsesPonytail(%s) = %v, want %v", tc.agent, got, tc.want)
			}
		})
	}
}
