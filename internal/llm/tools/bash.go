package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/kaiau00/aux-cli/internal/llm/tools/shell"
	"github.com/kaiau00/aux-cli/internal/permission"
)

type BashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type BashPermissionsParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type BashResponseMetadata struct {
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
}
type bashTool struct {
	permissions permission.Service
}

const (
	BashToolName = "bash"

	DefaultTimeout  = 1 * 60 * 1000  // 1 minutes in milliseconds
	MaxTimeout      = 10 * 60 * 1000 // 10 minutes in milliseconds
	MaxOutputLength = 30000
)

var bannedCommands = []string{
	"alias", "curl", "curlie", "wget", "axel", "aria2c",
	"nc", "telnet", "lynx", "w3m", "links", "httpie", "xh",
	"http-prompt", "chrome", "firefox", "safari",
}

// safeCommand is a command prefix that may run without a permission prompt.
// argsOK, when set, must accept the whitespace-separated words after the
// prefix; nil means no argument can make the command mutate anything.
type safeCommand struct {
	prefix string
	argsOK func(args []string) bool
}

// safeReadOnlyCommands run without a permission prompt, and only when the
// command contains none of shellOperators or disguisingChars. Nothing that
// wraps another command (env, timeout, nohup, …) or executes project code
// (go test, go run, …) belongs here: either turns the prefix match into "run
// anything".
var safeReadOnlyCommands = []safeCommand{
	{prefix: "ls"}, {prefix: "echo"}, {prefix: "pwd"}, {prefix: "cal"}, {prefix: "uptime"}, {prefix: "whoami"},
	{prefix: "id"}, {prefix: "groups"}, {prefix: "printenv"}, {prefix: "which"}, {prefix: "type"}, {prefix: "whereis"},
	{prefix: "whatis"}, {prefix: "uname"}, {prefix: "df"}, {prefix: "du"}, {prefix: "free"}, {prefix: "ps"},
	{prefix: "date", argsOK: dateDisplayOnly},
	{prefix: "hostname", argsOK: onlyFlags("-s", "--short", "-f", "--fqdn", "--long", "-d", "--domain",
		"-i", "--ip-address", "-I", "--all-ip-addresses")},

	{prefix: "git status", argsOK: gitReadOnlyArgs},
	{prefix: "git log", argsOK: gitReadOnlyArgs},
	{prefix: "git diff", argsOK: gitReadOnlyArgs},
	{prefix: "git show", argsOK: gitReadOnlyArgs},
	{prefix: "git branch", argsOK: onlyFlags("-a", "--all", "-r", "--remotes", "-v", "-vv", "--verbose", "-l", "--list",
		"--show-current", "--merged", "--no-merged", "--contains", "--no-contains", "--points-at", "--sort", "--format",
		"--color", "--no-color", "--column", "--no-column")},
	{prefix: "git tag", argsOK: onlyFlags("-l", "--list", "-n", "--sort", "--format", "--merged", "--no-merged",
		"--contains", "--no-contains", "--points-at", "--color", "--column", "--no-column", "-i", "--ignore-case")},
	{prefix: "git remote", argsOK: gitRemoteReadOnly},
	{prefix: "git ls-files", argsOK: gitReadOnlyArgs},
	{prefix: "git ls-remote", argsOK: gitReadOnlyArgs},
	{prefix: "git rev-parse", argsOK: gitReadOnlyArgs},
	{prefix: "git config --get", argsOK: gitReadOnlyArgs},
	{prefix: "git config --list", argsOK: gitReadOnlyArgs},
	{prefix: "git describe", argsOK: gitReadOnlyArgs},
	{prefix: "git blame", argsOK: gitReadOnlyArgs},
	{prefix: "git grep", argsOK: gitReadOnlyArgs},
	{prefix: "git shortlog", argsOK: gitReadOnlyArgs},

	{prefix: "go version"}, {prefix: "go help"}, {prefix: "go doc"},
	{prefix: "go list", argsOK: goFlagsOnly("json", "f", "m", "e", "deps", "find", "test", "versions", "u", "retracted")},
	{prefix: "go env", argsOK: goFlagsOnly("json", "changed")},
}

// shellOperators chain, substitute, redirect, or background commands. Any of
// them lets a safe-looking prefix carry a second command, so their presence
// disqualifies the fast path.
var shellOperators = []string{";", "&&", "||", "|", "`", "$(", "${", "\n", ">", "<", "&"}

// disguisingChars let the shell turn a harmless-looking word into a flag:
// '--output=x', --out\put=x, {--output=x,} and $'\x2d-output=x' all reach git
// as --output=x. Refusing them keeps the argument rules a whitespace split.
const disguisingChars = `'"\{}$`

// gitDangerousLongOptions mutate, execute a program, or print a file named in
// the arguments. Git accepts unambiguous abbreviations, so any long option
// that is a prefix of one of these is refused too.
var gitDangerousLongOptions = []string{
	"output", "upload-pack", "exec", "no-index", "ext-diff", "open-files-in-pager", "contents", "file",
}

// isSafeReadOnly reports whether command may run without asking the user.
func isSafeReadOnly(command string) bool {
	for _, op := range shellOperators {
		if strings.Contains(command, op) {
			return false
		}
	}
	if strings.ContainsAny(command, disguisingChars) {
		return false
	}
	for _, safe := range safeReadOnlyCommands {
		p := safe.prefix
		if len(command) < len(p) || !strings.EqualFold(command[:len(p)], p) {
			continue
		}
		if len(command) == len(p) || command[len(p)] == ' ' || command[len(p)] == '-' {
			return safe.argsOK == nil || safe.argsOK(strings.Fields(command[len(p):]))
		}
	}
	return false
}

func gitReadOnlyArgs(args []string) bool {
	for _, arg := range args {
		switch {
		case arg == "--":
		case strings.HasPrefix(arg, "--"):
			name, _, _ := strings.Cut(arg[2:], "=")
			for _, dangerous := range gitDangerousLongOptions {
				if strings.HasPrefix(dangerous, name) {
					return false
				}
			}
		case strings.HasPrefix(arg, "-"):
			// -O opens matches in a program (git grep); -f reads a named file.
			if strings.ContainsAny(arg[1:], "Of") {
				return false
			}
		}
	}
	return true
}

func gitRemoteReadOnly(args []string) bool {
	for len(args) > 0 && (args[0] == "-v" || args[0] == "--verbose") {
		args = args[1:]
	}
	if len(args) == 0 {
		return true
	}
	return (args[0] == "show" || args[0] == "get-url") && gitReadOnlyArgs(args[1:])
}

// onlyFlags accepts arguments that are all listed flags, optionally with an
// =value. Positional arguments are refused: for git branch and git tag a
// bare name creates one.
func onlyFlags(allowed ...string) func([]string) bool {
	return func(args []string) bool {
		for _, arg := range args {
			name, _, _ := strings.Cut(arg, "=")
			if !strings.HasPrefix(arg, "-") || !slices.Contains(allowed, name) {
				return false
			}
		}
		return true
	}
}

// goFlagsOnly accepts positional arguments and the listed flags. The go
// command treats -x and --x alike, and -toolexec or -w must never get through.
func goFlagsOnly(allowed ...string) func([]string) bool {
	return func(args []string) bool {
		for _, arg := range args {
			if !strings.HasPrefix(arg, "-") {
				continue
			}
			name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
			if !slices.Contains(allowed, name) {
				return false
			}
		}
		return true
	}
}

// dateDisplayOnly accepts +format and display flags; any other argument may
// set the clock (date -s, date MMDDhhmm).
func dateDisplayOnly(args []string) bool {
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "+"):
		case arg == "-u", arg == "--utc", arg == "--universal", arg == "-R", arg == "--rfc-email":
		case strings.HasPrefix(arg, "-I"), strings.HasPrefix(arg, "--iso-8601"), strings.HasPrefix(arg, "--rfc-3339"):
		default:
			return false
		}
	}
	return true
}

// bannedCommandIn returns the first banned command appearing anywhere in
// command, not only in first position, so `ls && curl x` is caught.
func bannedCommandIn(command string) (string, bool) {
	words := strings.FieldsFunc(command, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(";&|`<>$(){}", r)
	})
	for _, word := range words {
		for _, banned := range bannedCommands {
			if strings.EqualFold(word, banned) {
				return word, true
			}
		}
	}
	return "", false
}

func bashDescription() string {
	bannedCommandsStr := strings.Join(bannedCommands, ", ")
	safePrefixes := make([]string, len(safeReadOnlyCommands))
	for i, safe := range safeReadOnlyCommands {
		safePrefixes[i] = safe.prefix
	}
	safeCommandsStr := strings.Join(safePrefixes, ", ")
	return fmt.Sprintf(`Executes a given bash command in a persistent shell session with optional timeout, ensuring proper handling and security measures.

Before executing the command, please follow these steps:

1. Directory Verification:
 - If the command will create new directories or files, first use the LS tool to verify the parent directory exists and is the correct location
 - For example, before running "mkdir foo/bar", first use LS to check that "foo" exists and is the intended parent directory

2. Security Check:
 - For security and to limit the threat of a prompt injection attack, some commands are limited or banned. If you use a disallowed command, you will receive an error message explaining the restriction. Explain the error to the User.
 - Verify that the command does not use any of the banned commands, anywhere in it: %s.
 - Only a single command starting with one of these, with read-only arguments, runs without asking the user: %s. Any other command asks the user for permission first, and so does any command containing ;, &&, ||, |, &, a backtick, $, a quote, a backslash, a brace, a redirection (> or <), or a newline.

3. Command Execution:
 - After ensuring proper quoting, execute the command.
 - Capture the output of the command.

4. Output Processing:
 - If the output exceeds %d characters, output will be truncated before being returned to you.
 - Prepare the output for display to the user.

5. Return Result:
 - Provide the processed output of the command.
 - If any errors occurred during execution, include those in the output.

Usage notes:
- The command argument is required.
- You can specify an optional timeout in milliseconds (up to 600000ms / 10 minutes). If not specified, commands will timeout after 30 minutes.
- VERY IMPORTANT: You MUST avoid using search commands like 'find' and 'grep'. Instead use Grep, Glob, or Agent tools to search. You MUST avoid read tools like 'cat', 'head', 'tail', and 'ls', and use FileRead and LS tools to read files.
- When issuing multiple commands, use the ';' or '&&' operator to separate them. DO NOT use newlines (newlines are ok in quoted strings).
- IMPORTANT: All commands share the same shell session. Shell state (environment variables, virtual environments, current directory, etc.) persist between commands. For example, if you set an environment variable as part of a command, the environment variable will persist for subsequent commands.
- Try to maintain your current working directory throughout the session by using absolute paths and avoiding usage of 'cd'. You may use 'cd' if the User explicitly requests it.
<good-example>
pytest /foo/bar/tests
</good-example>
<bad-example>
cd /foo/bar && pytest tests
</bad-example>

# Committing changes with git

When the user asks you to create a new git commit, follow these steps carefully:

1. Start with a single message that contains exactly three tool_use blocks that do the following (it is VERY IMPORTANT that you send these tool_use blocks in a single message, otherwise it will feel slow to the user!):
 - Run a git status command to see all untracked files.
 - Run a git diff command to see both staged and unstaged changes that will be committed.
 - Run a git log command to see recent commit messages, so that you can follow this repository's commit message style.

2. Use the git context at the start of this conversation to determine which files are relevant to your commit. Add relevant untracked files to the staging area. Do not commit files that were already modified at the start of this conversation, if they are not relevant to your commit.

3. Analyze all staged changes (both previously staged and newly added) and draft a commit message. Wrap your analysis process in <commit_analysis> tags:

<commit_analysis>
- List the files that have been changed or added
- Summarize the nature of the changes (eg. new feature, enhancement to an existing feature, bug fix, refactoring, test, docs, etc.)
- Brainstorm the purpose or motivation behind these changes
- Do not use tools to explore code, beyond what is available in the git context
- Assess the impact of these changes on the overall project
- Check for any sensitive information that shouldn't be committed
- Draft a concise (1-2 sentences) commit message that focuses on the "why" rather than the "what"
- Ensure your language is clear, concise, and to the point
- Ensure the message accurately reflects the changes and their purpose (i.e. "add" means a wholly new feature, "update" means an enhancement to an existing feature, "fix" means a bug fix, etc.)
- Ensure the message is not generic (avoid words like "Update" or "Fix" without context)
- Review the draft message to ensure it accurately reflects the changes and their purpose
</commit_analysis>

4. Create the commit with a message ending with:
🤖 Generated with aux
Co-Authored-By: aux <noreply@aux.ai>

- In order to ensure good formatting, ALWAYS pass the commit message via a HEREDOC, a la this example:
<example>
git commit -m "$(cat <<'EOF'
 Commit message here.

 🤖 Generated with aux
 Co-Authored-By: aux <noreply@aux.ai>
 EOF
 )"
</example>

5. If the commit fails due to pre-commit hook changes, retry the commit ONCE to include these automated changes. If it fails again, it usually means a pre-commit hook is preventing the commit. If the commit succeeds but you notice that files were modified by the pre-commit hook, you MUST amend your commit to include them.

6. Finally, run git status to make sure the commit succeeded.

Important notes:
- When possible, combine the "git add" and "git commit" commands into a single "git commit -am" command, to speed things up
- However, be careful not to stage files (e.g. with 'git add .') for commits that aren't part of the change, they may have untracked files they want to keep around, but not commit.
- NEVER update the git config
- DO NOT push to the remote repository
- IMPORTANT: Never use git commands with the -i flag (like git rebase -i or git add -i) since they require interactive input which is not supported.
- If there are no changes to commit (i.e., no untracked files and no modifications), do not create an empty commit
- Ensure your commit message is meaningful and concise. It should explain the purpose of the changes, not just describe them.
- Return an empty response - the user will see the git output directly

# Creating pull requests
Use the gh command via the Bash tool for ALL GitHub-related tasks including working with issues, pull requests, checks, and releases. If given a Github URL use the gh command to get the information needed.

IMPORTANT: When the user asks you to create a pull request, follow these steps carefully:

1. Understand the current state of the branch. Remember to send a single message that contains multiple tool_use blocks (it is VERY IMPORTANT that you do this in a single message, otherwise it will feel slow to the user!):
 - Run a git status command to see all untracked files.
 - Run a git diff command to see both staged and unstaged changes that will be committed.
 - Check if the current branch tracks a remote branch and is up to date with the remote, so you know if you need to push to the remote
 - Run a git log command and 'git diff main...HEAD' to understand the full commit history for the current branch (from the time it diverged from the 'main' branch.)

2. Create new branch if needed

3. Commit changes if needed

4. Push to remote with -u flag if needed

5. Analyze all changes that will be included in the pull request, making sure to look at all relevant commits (not just the latest commit, but all commits that will be included in the pull request!), and draft a pull request summary. Wrap your analysis process in <pr_analysis> tags:

<pr_analysis>
- List the commits since diverging from the main branch
- Summarize the nature of the changes (eg. new feature, enhancement to an existing feature, bug fix, refactoring, test, docs, etc.)
- Brainstorm the purpose or motivation behind these changes
- Assess the impact of these changes on the overall project
- Do not use tools to explore code, beyond what is available in the git context
- Check for any sensitive information that shouldn't be committed
- Draft a concise (1-2 bullet points) pull request summary that focuses on the "why" rather than the "what"
- Ensure the summary accurately reflects all changes since diverging from the main branch
- Ensure your language is clear, concise, and to the point
- Ensure the summary accurately reflects the changes and their purpose (ie. "add" means a wholly new feature, "update" means an enhancement to an existing feature, "fix" means a bug fix, etc.)
- Ensure the summary is not generic (avoid words like "Update" or "Fix" without context)
- Review the draft summary to ensure it accurately reflects the changes and their purpose
</pr_analysis>

6. Create PR using gh pr create with the format below. Use a HEREDOC to pass the body to ensure correct formatting.
<example>
gh pr create --title "the pr title" --body "$(cat <<'EOF'
## Summary
<1-3 bullet points>

## Test plan
[Checklist of TODOs for testing the pull request...]

🤖 Generated with aux
EOF
)"
</example>

Important:
- Return an empty response - the user will see the gh output directly
- Never update git config`, bannedCommandsStr, safeCommandsStr, MaxOutputLength)
}

func NewBashTool(permission permission.Service) BaseTool {
	return &bashTool{
		permissions: permission,
	}
}

func (b *bashTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BashToolName,
		Description: bashDescription(),
		Parameters: map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Optional timeout in milliseconds (max 600000)",
			},
		},
		Required: []string{"command"},
	}
}

func (b *bashTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params BashParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("invalid parameters"), nil
	}

	if params.Timeout > MaxTimeout {
		params.Timeout = MaxTimeout
	} else if params.Timeout <= 0 {
		params.Timeout = DefaultTimeout
	}

	if params.Command == "" {
		return NewTextErrorResponse("missing command"), nil
	}

	if banned, ok := bannedCommandIn(params.Command); ok {
		return NewTextErrorResponse(fmt.Sprintf("command '%s' is not allowed", banned)), nil
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}
	if !isSafeReadOnly(params.Command) {
		p := b.permissions.Request(
			permission.CreatePermissionRequest{
				SessionID:   sessionID,
				Path:        ResolveWorkingDir(ctx),
				ToolName:    BashToolName,
				Action:      "execute",
				Description: fmt.Sprintf("Execute command: %s", params.Command),
				Params: BashPermissionsParams{
					Command: params.Command,
				},
				// Path is the working directory for every command, so without
				// the command itself in the key, approving one command for the
				// session would authorize every later command in that session.
				Fingerprint: params.Command,
			},
		)
		if !p {
			return ToolResponse{}, permission.ErrorPermissionDenied
		}
	}
	startTime := time.Now()
	shell := shell.GetPersistentShell(ResolveWorkingDir(ctx))
	stdout, stderr, exitCode, interrupted, err := shell.Exec(ctx, params.Command, params.Timeout)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error executing command: %w", err)
	}

	stdout = truncateOutput(stdout)
	stderr = truncateOutput(stderr)

	errorMessage := stderr
	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	metadata := BashResponseMetadata{
		StartTime: startTime.UnixMilli(),
		EndTime:   time.Now().UnixMilli(),
	}
	if stdout == "" {
		return WithResponseMetadata(NewTextResponse("no output"), metadata), nil
	}
	return WithResponseMetadata(NewTextResponse(stdout), metadata), nil
}

func truncateOutput(content string) string {
	if len(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]

	truncatedLinesCount := countLines(content[halfLength : len(content)-halfLength])
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLinesCount, end)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}
