package nvsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const activeRouteConfig = `# included in /Users/test/.ssh/config
# brev-external-node node_id=node-123 alias=my-spark-mdns
Host my-spark-mdns
  HostName tunnel.example
  User ubuntu
  Port 22022
  IdentityFile "/Users/test/.brev/brev.pem"
  ProxyCommand none

Host unrelated-workspace
  HostName workspace.example
`

func TestInspectRouteConfigRecognizesOwnedActiveRoute(t *testing.T) {
	got := inspectRouteConfig(activeRouteConfig, "my-spark-mdns", "my-spark-mdns")
	want := RouteResult{
		SchemaVersion: 1,
		NodeName:      "my-spark-mdns",
		SSHAlias:      "my-spark-mdns",
		State:         "active",
		Ownership:     "brev",
	}
	if got != want {
		t.Fatalf("route result = %#v, want %#v", got, want)
	}
}

func TestInspectRouteConfigRejectsOwnedRouteWithoutProxyNeutralization(t *testing.T) {
	config := `# brev-external-node node_id=node-123 alias=my-spark-mdns
Host my-spark-mdns
  HostName tunnel.example
`

	got := inspectRouteConfig(config, "my-spark-mdns", "my-spark-mdns")
	if got.State != "invalid" || got.Ownership != "brev" {
		t.Fatalf("route result = %#v, want invalid Brev-owned route", got)
	}
}

func TestInspectRouteConfigRejectsOwnershipMarkerWithoutNodeID(t *testing.T) {
	config := `# brev-external-node alias=my-spark-mdns
Host my-spark-mdns
  ProxyCommand none
`

	got := inspectRouteConfig(config, "my-spark-mdns", "my-spark-mdns")
	if got.State != "invalid" || got.Ownership != "brev" {
		t.Fatalf("route result = %#v, want invalid Brev-owned route", got)
	}
}

func TestSSHRouteStatusIsLocalAndEmitsOnlyMachineJSON(t *testing.T) {
	deps := routeDependencies{
		prepare: func(context.Context, string, string) error {
			t.Fatal("status must not prepare or call the backend")
			return nil
		},
		refresh: func() error {
			t.Fatal("status must not refresh or call the backend")
			return nil
		},
		readConfig: func() (string, error) { return activeRouteConfig, nil },
	}
	cmd := newCmdNVSYNC(deps)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ssh-route", "status", "--node", "my-spark-mdns", "--alias", "my-spark-mdns"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	var got RouteResult
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode output %q: %v", stdout.String(), err)
	}
	if got.State != "active" || got.Ownership != "brev" || got.SchemaVersion != 1 {
		t.Fatalf("route result = %#v", got)
	}
}

func TestSSHRouteConfigureFailsWithoutVerifiedOwnedEntry(t *testing.T) {
	deps := routeDependencies{
		prepare:    func(context.Context, string, string) error { return nil },
		refresh:    func() error { return nil },
		readConfig: func() (string, error) { return "Host unrelated\n", nil },
	}
	cmd := newCmdNVSYNC(deps)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ssh-route", "configure", "--node", "my-spark-mdns", "--alias", "my-spark-mdns"})

	err := cmd.Execute()
	if err == nil || !errors.Is(err, errRouteVerification) {
		t.Fatalf("configure error = %v, want route verification failure", err)
	}
}

func TestSSHRouteConfigureEmitsActiveOnlyAfterOrderedVerification(t *testing.T) {
	steps := make([]string, 0, 3)
	deps := routeDependencies{
		prepare: func(context.Context, string, string) error {
			steps = append(steps, "prepare")
			return nil
		},
		refresh: func() error {
			steps = append(steps, "refresh")
			return nil
		},
		readConfig: func() (string, error) {
			steps = append(steps, "verify")
			return activeRouteConfig, nil
		},
	}
	cmd := newCmdNVSYNC(deps)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ssh-route", "configure", "--node", "my-spark-mdns", "--alias", "my-spark-mdns"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("configure failed: %v", err)
	}
	if got := strings.Join(steps, ","); got != "prepare,refresh,verify" {
		t.Fatalf("steps = %q", got)
	}
	if !strings.Contains(stdout.String(), `"state":"active"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSSHRouteRemoveRefreshesThenVerifiesAbsence(t *testing.T) {
	refreshed := false
	deps := routeDependencies{
		prepare: func(context.Context, string, string) error {
			t.Fatal("remove must not prepare an existing backend node")
			return nil
		},
		refresh: func() error {
			refreshed = true
			return nil
		},
		readConfig: func() (string, error) {
			if !refreshed {
				t.Fatal("remove verified before refresh")
			}
			return "Host unrelated\n", nil
		},
	}
	cmd := newCmdNVSYNC(deps)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ssh-route", "remove", "--node", "my-spark-mdns", "--alias", "my-spark-mdns"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if !strings.Contains(stdout.String(), `"state":"absent"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
