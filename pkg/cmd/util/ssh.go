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
	"github.com/brevdev/brev-cli/pkg/terminal"
	"github.com/briandowns/spinner"
)

var (
	sshAvailabilityConnectTimeoutSeconds = 3
	sshAvailabilityAttemptTimeout        = 5 * time.Second
	sshAvailabilityWaitDelay             = time.Second
	sshAvailabilityRetrySleep            = time.Second
	sshAvailabilityMaxAttempts           = 20
)

// WorkspacePollingStore is the minimal interface needed for polling workspace state
type WorkspacePollingStore interface {
	GetWorkspace(workspaceID string) (*entity.Workspace, error)
}

// WorkspaceStartStore is the interface needed for starting stopped workspaces
type WorkspaceStartStore interface {
	GetWorkspaceByNameOrIDErrStore
	WorkspacePollingStore
	StartWorkspace(workspaceID string) (*entity.Workspace, error)
}

// pollInterval is how long PollUntil waits between status checks.
const pollInterval = 5 * time.Second

// PollUntil polls the workspace status until it matches the desired state or times out
func PollUntil(s *spinner.Spinner, wsid string, state string, pollingStore WorkspacePollingStore, waitMsg string, timeout time.Duration) error {
	s.Suffix = waitMsg
	s.Start()
	defer s.Stop()
	deadline := time.Now().Add(timeout)
	for {
		// Check before sleeping so an instance already in the target state
		// returns immediately instead of waiting out one poll interval.
		ws, err := pollingStore.GetWorkspace(wsid)
		if err != nil {
			return breverrors.WrapAndTrace(err)
		}
		if ws.Status == state {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return breverrors.WrapAndTrace(fmt.Errorf("timed out waiting for instance to reach %s state after %v", state, timeout))
		}
		s.Suffix = waitMsg
		// Never sleep past the deadline, so timeouts are reported on time.
		time.Sleep(min(remaining, pollInterval))
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

// StartWorkspaceIfStopped starts a workspace and waits for it to be running,
// returning the refreshed workspace so callers neither re-fetch nor re-poll it.
func StartWorkspaceIfStopped(t *terminal.Terminal, s *spinner.Spinner, tstore WorkspaceStartStore, wsIDOrName string, workspace *entity.Workspace, timeout time.Duration) (*entity.Workspace, error) {
	activeOrg, err := tstore.GetActiveOrganizationOrDefault()
	if err != nil {
		return nil, breverrors.WrapAndTrace(err)
	}
	workspaces, err := tstore.GetWorkspaceByNameOrID(activeOrg.ID, wsIDOrName)
	if err != nil {
		return nil, breverrors.WrapAndTrace(err)
	}
	startedWorkspace, err := tstore.StartWorkspace(workspaces[0].ID)
	if err != nil {
		return nil, breverrors.WrapAndTrace(err)
	}
	t.Vprintf("%s", t.Yellow("Instance %s is starting. \n\n", startedWorkspace.Name))
	err = PollUntil(s, workspace.ID, entity.Running, tstore, " hang tight 🤙", timeout)
	if err != nil {
		return nil, breverrors.WrapAndTrace(err)
	}
	refreshed, err := GetUserWorkspaceByNameOrIDErr(tstore, wsIDOrName)
	if err != nil {
		return nil, breverrors.WrapAndTrace(err)
	}
	return refreshed, nil
}
