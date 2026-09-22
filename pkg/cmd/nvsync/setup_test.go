package nvsync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
)

type setupCall struct {
	stage string
	args  []string
}

func recordingSetupStage(calls *[]setupCall, stage string, result error) setupStage {
	return func(_ context.Context, _ io.Reader, _ io.Writer, _ io.Writer, args []string) error {
		*calls = append(*calls, setupCall{stage: stage, args: append([]string(nil), args...)})
		return result
	}
}

func TestSetupTargetRunsRegisterAndEnableSSHInOneCommand(t *testing.T) {
	calls := make([]setupCall, 0, 2)
	cmd := newSetupTargetCommand(setupDependencies{
		register:  recordingSetupStage(&calls, "register", nil),
		enableSSH: recordingSetupStage(&calls, "enable-ssh", nil),
	})
	cmd.SetIn(bytes.NewBufferString("interactive input"))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--alias", "my-spark-mdns"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("setup-target failed: %v", err)
	}
	want := []setupCall{
		{stage: "register", args: []string{"--fixed-name", "my-spark-mdns"}},
		{stage: "enable-ssh"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("setup-target calls = %#v, want %#v", calls, want)
	}
}

func TestSetupTargetStopsAtEnableSSHFailureWithStableExitCode(t *testing.T) {
	wantErr := errors.New("enable SSH failed")
	calls := make([]setupCall, 0, 2)
	cmd := newSetupTargetCommand(setupDependencies{
		register:  recordingSetupStage(&calls, "register", nil),
		enableSSH: recordingSetupStage(&calls, "enable-ssh", wantErr),
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--alias", "my-spark-mdns"})

	err := cmd.ExecuteContext(t.Context())
	var exitErr interface{ ExitCode() int }
	if !errors.Is(err, wantErr) || !errors.As(err, &exitErr) || exitErr.ExitCode() != setupEnableSSHExitCode {
		t.Fatalf("setup-target error = %v with exit code %v, want wrapped error and %d", err, exitErr, setupEnableSSHExitCode)
	}
}

func TestSetupHostRunsGrantAndRouteConfigurationInOneCommand(t *testing.T) {
	calls := make([]setupCall, 0, 2)
	cmd := newSetupHostCommand(setupDependencies{
		grantSSH:       recordingSetupStage(&calls, "grant-ssh", nil),
		configureRoute: recordingSetupStage(&calls, "configure-route", nil),
	})
	cmd.SetIn(bytes.NewBufferString("interactive input"))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--node", "my-spark-mdns", "--alias", "my-spark-mdns", "--linux-user", "nvidia"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("setup-host failed: %v", err)
	}
	want := []setupCall{
		{stage: "grant-ssh", args: []string{"--interactive", "--self", "--node", "my-spark-mdns", "--linux-user", "nvidia"}},
		{stage: "configure-route", args: []string{"ssh-route", "configure", "--node", "my-spark-mdns", "--alias", "my-spark-mdns"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("setup-host calls = %#v, want %#v", calls, want)
	}
}

func TestSetupHostStopsBeforeRouteAfterGrantFailure(t *testing.T) {
	wantErr := errors.New("grant SSH failed")
	calls := make([]setupCall, 0, 2)
	cmd := newSetupHostCommand(setupDependencies{
		grantSSH: recordingSetupStage(&calls, "grant-ssh", wantErr),
		configureRoute: func(context.Context, io.Reader, io.Writer, io.Writer, []string) error {
			t.Fatal("route configuration ran after grant failure")
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--node", "my-spark-mdns", "--alias", "my-spark-mdns", "--linux-user", "nvidia"})

	err := cmd.ExecuteContext(t.Context())
	var exitErr interface{ ExitCode() int }
	if !errors.Is(err, wantErr) || !errors.As(err, &exitErr) || exitErr.ExitCode() != setupGrantSSHExitCode {
		t.Fatalf("setup-host error = %v with exit code %v, want wrapped error and %d", err, exitErr, setupGrantSSHExitCode)
	}
}
