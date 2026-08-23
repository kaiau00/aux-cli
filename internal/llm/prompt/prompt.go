package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/logging"
)

func GetAgentPrompt(agentName config.AgentName, provider models.ModelProvider) string {
	basePrompt := ""
	switch agentName {
	case config.AgentCoder:
		basePrompt = CoderPrompt(provider)
	case config.AgentTitle:
		basePrompt = TitlePrompt(provider)
	case config.AgentTask:
		basePrompt = TaskPrompt(provider)
	case config.AgentSummarizer:
		basePrompt = SummarizerPrompt(provider)
	default:
		basePrompt = "You are a helpful assistant"
	}

	// Ponytail Protocol and project-specific context files are scoped to the
	// agents that actually write code. Title and Summarizer must remain
	// unopinionated to keep session titles and summaries clean.
	if !agentUsesPonytail(agentName) {
		return basePrompt
	}

	if cfg := config.Get(); cfg == nil || !cfg.Ponytail.Enabled {
		// Ponytail disabled — still inject project context if present, just
		// without the YAGNI doctrine.
		if contextContent := getContextFromPaths(); contextContent != "" {
			return fmt.Sprintf("%s\n\n# Project-Specific Context\n Make sure to follow the instructions in the context below\n%s", basePrompt, contextContent)
		}
		return basePrompt
	}

	if contextContent := getContextFromPaths(); contextContent != "" {
		logging.Debug("Context content", "Context", contextContent)
		return appendPonytailProtocol(fmt.Sprintf("%s\n\n# Project-Specific Context\n Make sure to follow the instructions in the context below\n%s", basePrompt, contextContent))
	}
	return appendPonytailProtocol(basePrompt)
}

// agentUsesPonytail reports whether the Ponytail Protocol should be appended
// to the given agent's prompt. Only Coder and Task receive it; Title and
// Summarizer stay unopinionated.
func agentUsesPonytail(agentName config.AgentName) bool {
	return agentName == config.AgentCoder || agentName == config.AgentTask
}

const ponytailProtocol = `### EXECUTION MANDATE: THE PONYTAIL PROTOCOL

Before generating or modifying code, evaluate this YAGNI decision ladder in order:

1. Does this feature actually need to exist? If no, reject the request.
2. Is this logic already implemented elsewhere in the codebase? If yes, reuse it; do not rewrite it.
3. Can the standard language library handle this without adding an external dependency? If yes, use the stdlib exclusively.
4. Does a native platform feature cover this? If yes, use the native feature.
5. If code must be written, write the absolute minimum number of lines required to fulfill the logic. Over-engineering will be penalized.

When skipping an implementation or choosing simpler logic, prepend the relevant code or response with a brief ponytail comment explaining the lazy architectural choice, for example:
# ponytail: stdlib provides this function natively.`

func appendPonytailProtocol(prompt string) string {
	return fmt.Sprintf("%s\n\n%s", prompt, ponytailProtocol)
}

func getContextFromPaths() string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	return processContextPaths(cfg.WorkingDir, cfg.ContextPaths)
}

// processContextPaths reads the configured context files and concatenates them
// in the order the paths were configured.
//
// Order is load-bearing rather than cosmetic. This text goes into the system
// prompt, which is the prefix providers key their cache on, and a prefix that
// differs between runs throws that cache away — measured on this project, a
// stable prefix is served ~99% from cache and accounts for the large majority
// of input tokens. An earlier version fanned out a goroutine per path and
// collected through a shared channel, so the order was whatever order the
// goroutines happened to finish in.
//
// Each path still gets its own goroutine; results are written to that path's
// own slot instead of a shared channel, so parallelism is kept and the output
// is deterministic. Deduplication remains case-insensitive and global, and the
// first configured path that mentions a file wins.
func processContextPaths(workDir string, paths []string) string {
	var (
		wg      sync.WaitGroup
		perPath = make([][]string, len(paths))

		processedFiles = make(map[string]bool)
		processedMutex sync.Mutex
	)

	// claim reports whether this call is the first to see a file. Claiming
	// happens as files are discovered, so which path wins a duplicate can
	// depend on timing; the *order of the output* does not.
	claim := func(path string) bool {
		processedMutex.Lock()
		defer processedMutex.Unlock()
		lower := strings.ToLower(path)
		if processedFiles[lower] {
			return false
		}
		processedFiles[lower] = true
		return true
	}

	for i, path := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()

			if strings.HasSuffix(p, "/") {
				// WalkDir visits lexically, so a directory's own contents are
				// already in a stable order.
				_ = filepath.WalkDir(filepath.Join(workDir, p), func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() || !claim(path) {
						return nil
					}
					if result := processFile(path); result != "" {
						perPath[i] = append(perPath[i], result)
					}
					return nil
				})
				return
			}

			fullPath := filepath.Join(workDir, p)
			if !claim(fullPath) {
				return
			}
			if result := processFile(fullPath); result != "" {
				perPath[i] = append(perPath[i], result)
			}
		}(i, path)
	}
	wg.Wait()

	results := make([]string, 0, len(paths))
	for _, group := range perPath {
		results = append(results, group...)
	}
	return strings.Join(results, "\n")
}

func processFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return "# From:" + filePath + "\n" + string(content)
}
