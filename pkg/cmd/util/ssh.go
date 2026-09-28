package util

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/brevdev/brev-cli/pkg/entity"
	breverrors "github.com/brevdev/brev-cli/pkg/errors"
	"github.com/brevdev/brev-cli/pkg/store"
	"github.com/briandowns/spinner"
)

var (
	sshAvailabilityConnectTimeoutSeconds = 3
	sshAvailabilityAttemptTimeout        = 5 * time.Second
	sshAvailabilityWaitDelay             = time.Second
	sshAvailabilityRetrySleep            = time.Second
	// Only transient "host is not accepting connections yet" failures are worth
	// retrying, and only briefly: anything longer is the user's call, not ours.
	sshAvailabilityMaxAttempts = 3
)

// RequireRunning returns an actionable error when the workspace cannot be
// reached over SSH. Callers surface it instead of waiting for a starting
// instance or starting a stopped one for the user.
func RequireRunning(workspace *entity.Workspace) error {
	switch workspace.Status {
	case entity.Running:
		return nil
	case entity.Stopped:
		return breverrors.NewValidationError(fmt.Sprintf(
			"instance %s is not running, please start it with: brev start %s",
			workspace.Name, workspace.Name))
	default:
		return breverrors.NewValidationError(fmt.Sprintf(
			"instance %s is not running (status: %s); run 'brev ls' to check on it",
			workspace.Name, workspace.Status))
	}
}

// WaitForSSHToBeAvailable polls until an SSH connection can be established
func WaitForSSHToBeAvailable(sshAlias string, s *spinner.Spinner) error {
	s.Suffix = " waiting for SSH connection to be available"
	s.Start()
	defer s.Stop()
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), sshAvailabilityAttemptTimeout)
		cmd := exec.CommandContext(ctx, "ssh",
			"-T",
			"-o", fmt.Sprintf("ConnectTimeout=%d", sshAvailabilityConnectTimeoutSeconds),
			"-o", "ConnectionAttempts=1",
			"-o", "BatchMode=yes",
			"-o", "NumberOfPasswordPrompts=0",
			"-o", "RequestTTY=no",
			"-o", "LogLevel=ERROR",
			sshAlias,
			"true",
		)
		cmd.WaitDelay = sshAvailabilityWaitDelay
		out, err := cmd.CombinedOutput()
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		if err == nil {
			return nil
		}

		stdErr := strings.TrimSpace(string(out))
		if timedOut {
			stdErr = fmt.Sprintf("SSH attempt %d timed out after %s", attempt, sshAvailabilityAttemptTimeout)
		} else if stdErr == "" {
			stdErr = err.Error()
		}

		// A failure that will not resolve by waiting (bad credentials, host key
		// mismatch) fails immediately; otherwise keep trying up to the budget.
		if !timedOut && !store.SatisfactorySSHErrMessage(stdErr) {
			return breverrors.WrapAndTrace(errors.New("\n" + stdErr))
		}
		if attempt >= sshAvailabilityMaxAttempts {
			return breverrors.WrapAndTrace(fmt.Errorf(
				"SSH to %s was not available after %d attempts: %s", sshAlias, attempt, stdErr))
		}

		s.Stop()
		_, _ = fmt.Fprintf(s.Writer, "still waiting for SSH connection (attempt %d failed; retrying)\n", attempt)
		time.Sleep(sshAvailabilityRetrySleep)
		s.Start()
	}
}
