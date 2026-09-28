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

	"github.com/brevdev/brev-cli/pkg/entity"
)

func TestRequireRunning(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		wantErr    bool
		wantSubstr []string
	}{
		{
			name:   "running is ready",
			status: entity.Running,
		},
		{
			name:       "stopped tells the user how to start it",
			status:     entity.Stopped,
			wantErr:    true,
			wantSubstr: []string{"is not running", "please start it with: brev start my-box"},
		},
		{
			name:       "mid-boot names the status instead of suggesting start",
			status:     entity.Starting,
			wantErr:    true,
			wantSubstr: []string{"is not running (status: STARTING)", "brev ls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireRunning(&entity.Workspace{Name: "my-box", Status: tt.status})

			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, substr := range tt.wantSubstr {
				assert.Contains(t, err.Error(), substr)
			}
		})
	}
}

// Retryable failures are attempted exactly sshAvailabilityMaxAttempts times, then
// reported: callers must not wait longer than that.
func TestWaitForSSHToBeAvailable_StopsAfterMaxAttempts(t *testing.T) {
	dir := t.TempDir()
	attemptsFile := filepath.Join(dir, "attempts")
	fakeSSH := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\n" +
		"n=$(cat " + attemptsFile + " 2>/dev/null || echo 0)\n" +
		"echo $((n+1)) > " + attemptsFile + "\n" +
		"echo 'ssh: connect to host my-box port 22: Connection refused' >&2\n" +
		"exit 255\n"
	require.NoError(t, os.WriteFile(fakeSSH, []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	originalSleep := sshAvailabilityRetrySleep
	sshAvailabilityRetrySleep = 0
	t.Cleanup(func() { sshAvailabilityRetrySleep = originalSleep })

	err := WaitForSSHToBeAvailable("my-box", spinner.New(spinner.CharSets[9], 100*time.Millisecond))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "was not available after 3 attempts")

	raw, readErr := os.ReadFile(attemptsFile)
	require.NoError(t, readErr)
	assert.Equal(t, "3", strings.TrimSpace(string(raw)), "transient failures must stop after the attempt budget")
}

// A failure that waiting cannot fix fails on the first attempt.
func TestWaitForSSHToBeAvailable_FailsFastOnPermanentError(t *testing.T) {
	dir := t.TempDir()
	attemptsFile := filepath.Join(dir, "attempts")
	fakeSSH := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\n" +
		"n=$(cat " + attemptsFile + " 2>/dev/null || echo 0)\n" +
		"echo $((n+1)) > " + attemptsFile + "\n" +
		"echo 'Host key verification failed.' >&2\n" +
		"exit 255\n"
	require.NoError(t, os.WriteFile(fakeSSH, []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := WaitForSSHToBeAvailable("my-box", spinner.New(spinner.CharSets[9], 100*time.Millisecond))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Host key verification failed")

	raw, readErr := os.ReadFile(attemptsFile)
	require.NoError(t, readErr)
	assert.Equal(t, "1", strings.TrimSpace(string(raw)), "a permanent failure must not be retried")
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
