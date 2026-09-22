package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briandowns/spinner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brevdev/brev-cli/pkg/auth"
	"github.com/brevdev/brev-cli/pkg/entity"
	"github.com/brevdev/brev-cli/pkg/terminal"
)

// pollCountingStore serves workspace status without any network access.
type pollCountingStore struct {
	status string
	calls  int
}

func (p *pollCountingStore) GetWorkspace(_ string) (*entity.Workspace, error) {
	p.calls++
	return &entity.Workspace{ID: "ws_1", Status: p.status}, nil
}

// startCountingStore advances a workspace from STOPPED to RUNNING on start.
type startCountingStore struct {
	workspace *entity.Workspace
	started   bool
	starts    int
}

func (s *startCountingStore) GetAuthTokens() (*entity.AuthTokens, error) { return nil, nil }

func (s *startCountingStore) GetCurrentUser() (*entity.User, error) {
	return &entity.User{ID: "user_1"}, nil
}

func (s *startCountingStore) GetActiveOrganizationOrDefault() (*entity.Organization, error) {
	return &entity.Organization{ID: "org_1"}, nil
}

func (s *startCountingStore) GetWorkspaceByNameOrID(_ string, _ string) ([]entity.Workspace, error) {
	return []entity.Workspace{*s.workspace}, nil
}

func (s *startCountingStore) GetWorkspace(_ string) (*entity.Workspace, error) {
	return s.workspace, nil
}

func (s *startCountingStore) StartWorkspace(_ string) (*entity.Workspace, error) {
	s.starts++
	s.started = true
	s.workspace.Status = entity.Running
	return s.workspace, nil
}

func testSpinner() *spinner.Spinner {
	return spinner.New(spinner.CharSets[9], 100*time.Millisecond)
}

// An instance that is already in the target state must not wait out a poll
// interval before the first status check.
func TestPollUntil_AlreadyInStateReturnsImmediately(t *testing.T) {
	store := &pollCountingStore{status: entity.Running}

	start := time.Now()
	err := PollUntil(testSpinner(), "ws_1", entity.Running, store, " waiting", time.Minute)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, 1, store.calls)
	assert.Less(t, elapsed, time.Second, "already-running instance waited for the poll interval")
}

// A timeout is reported without sleeping past the deadline.
func TestPollUntil_TimesOutOnTime(t *testing.T) {
	store := &pollCountingStore{status: "CREATING"}

	start := time.Now()
	err := PollUntil(testSpinner(), "ws_1", entity.Running, store, " waiting", 100*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out waiting for instance to reach RUNNING")
	assert.Less(t, elapsed, 2*time.Second, "timeout overshot its deadline")
}

// Starting a stopped instance returns the refreshed workspace, so the caller
// neither re-fetches nor polls it a second time.
func TestStartWorkspaceIfStopped_ReturnsRefreshedWorkspace(t *testing.T) {
	t.Setenv(auth.APIKeyEnvVar, "")
	store := &startCountingStore{workspace: &entity.Workspace{
		ID:              "ws_1",
		Name:            "box",
		Status:          "STOPPED",
		CreatedByUserID: "user_1",
	}}

	got, err := StartWorkspaceIfStopped(terminal.New(), testSpinner(), store, "box", store.workspace, time.Minute)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, entity.Running, got.Status)
	assert.Equal(t, 1, store.starts)
}

func TestWaitForSSHToBeAvailableTimesOutStuckSSHAttempt(t *testing.T) {
	dir := t.TempDir()
	fakeSSH := filepath.Join(dir, "ssh")
	err := os.WriteFile(fakeSSH, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755)
	if err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	originalAttemptTimeout := sshAvailabilityAttemptTimeout
	originalWaitDelay := sshAvailabilityWaitDelay
	originalRetrySleep := sshAvailabilityRetrySleep
	originalMaxAttempts := sshAvailabilityMaxAttempts
	sshAvailabilityAttemptTimeout = 50 * time.Millisecond
	sshAvailabilityWaitDelay = 50 * time.Millisecond
	sshAvailabilityRetrySleep = 0
	sshAvailabilityMaxAttempts = 0
	t.Cleanup(func() {
		sshAvailabilityAttemptTimeout = originalAttemptTimeout
		sshAvailabilityWaitDelay = originalWaitDelay
		sshAvailabilityRetrySleep = originalRetrySleep
		sshAvailabilityMaxAttempts = originalMaxAttempts
	})

	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	start := time.Now()
	err = WaitForSSHToBeAvailable("slow-host", s)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected stuck ssh attempt to fail")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected stuck ssh attempt to be killed quickly, took %v", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}
