package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/spf13/cobra"
)

// resumePickerSentinel is what --resume carries when it is given without an
// id. pflag only allows a flag to have an optional value by way of
// NoOptDefVal, and that setting changes how the value is parsed: measured
// against pflag v1.0.5, `--resume=abc` and `-r` behave, but `--resume abc` and
// `-r abc` leave the id in the positional arguments, and `-rabc` fails
// outright. So the id is recovered from the positional arguments below, and
// `-rabc` is the one spelling that does not work.
//
// pflag prints NoOptDefVal in `--help` as `--resume string[="ask"]`, so the
// sentinel has to be a word that reads as deliberate there and that a user can
// type and mean: `--resume=ask` does open the picker. Session ids are UUIDs,
// so "ask" cannot shadow a real one.
const resumePickerSentinel = "ask"

// registerResumeFlags declares --continue and --resume on cmd. The test
// registers them the same way, so a rename or a lost NoOptDefVal fails there
// rather than only in a real terminal.
func registerResumeFlags(cmd *cobra.Command) {
	cmd.Flags().BoolP("continue", "C", false, "Resume the most recent session in this project")
	cmd.Flags().StringP("resume", "r", "", "Resume a session by id; with no id, pick one from a list")
	// Makes the id optional. See resumePickerSentinel for what this costs.
	cmd.Flags().Lookup("resume").NoOptDefVal = resumePickerSentinel
}

// resumeAwareArgs accepts the single positional argument that `--resume <id>`
// leaves behind, and otherwise keeps cobra's complaint about an unknown
// subcommand.
//
// Needed because the root command has subcommands, so cobra's default rejects
// every positional argument: before this, `aux --resume abc` failed with
// `unknown command "abc" for "aux"` rather than resuming anything.
func resumeAwareArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 1 && cmd.Flags().Changed("resume") {
		if value, _ := cmd.Flags().GetString("resume"); value == resumePickerSentinel {
			return nil
		}
	}
	return cobra.NoArgs(cmd, args)
}

// resumeRequest is what the --continue and --resume flags asked for, before
// any session has been looked up. Separated from the lookup so the command
// line can be tested without a database.
type resumeRequest struct {
	mostRecent bool   // --continue
	sessionID  string // --resume <id>
	picker     bool   // --resume with no id
}

func (r resumeRequest) wanted() bool {
	return r.mostRecent || r.picker || r.sessionID != ""
}

// parseResumeFlags reads --continue and --resume off cmd.
func parseResumeFlags(cmd *cobra.Command, args []string) (resumeRequest, error) {
	var req resumeRequest
	req.mostRecent, _ = cmd.Flags().GetBool("continue")

	if cmd.Flags().Changed("resume") {
		value, _ := cmd.Flags().GetString("resume")
		if value == resumePickerSentinel {
			// See the sentinel's comment: `--resume abc` parks the id here.
			if len(args) > 0 && args[0] != "" {
				req.sessionID = args[0]
			} else {
				req.picker = true
			}
		} else {
			req.sessionID = value
		}
	}

	if req.mostRecent && (req.picker || req.sessionID != "") {
		return resumeRequest{}, errors.New("use either --continue or --resume, not both")
	}
	return req, nil
}

// resolveResume turns a request into the session to open. It returns the zero
// session when there is nothing to resume, which --continue treats as "start
// fresh" rather than as an error: a project with no history yet is the normal
// first run, not a mistake.
func resolveResume(ctx context.Context, sessions session.Service, req resumeRequest) (session.Session, error) {
	switch {
	case req.sessionID != "":
		sess, err := sessions.Get(ctx, req.sessionID)
		if err != nil {
			return session.Session{}, fmt.Errorf("no session %s in this project: %w", req.sessionID, err)
		}
		return sess, nil
	case req.mostRecent:
		return sessions.MostRecent(ctx)
	default:
		return session.Session{}, nil
	}
}
