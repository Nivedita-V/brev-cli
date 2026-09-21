package nvsync

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestSSHRouteProxyExecsExplicitBrevConfig(t *testing.T) {
	var gotPath string
	var gotArgv []string
	deps := routeDependencies{
		readConfig: func() (string, error) { return activeRouteConfig, nil },
		configPath: func() (string, error) { return "/Users/test/Brev Config/ssh_config", nil },
		execProxy: func(path string, argv []string, _ []string) error {
			gotPath = path
			gotArgv = append([]string(nil), argv...)
			return nil
		},
	}
	cmd := newCmdNVSYNC(deps)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ssh-route", "proxy", "--node", "my-spark-mdns", "--alias", "my-spark-mdns"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("proxy failed: %v", err)
	}
	if gotPath != "/usr/bin/ssh" {
		t.Fatalf("executable = %q, want /usr/bin/ssh", gotPath)
	}
	wantArgv := []string{
		"/usr/bin/ssh", "-F", "/Users/test/Brev Config/ssh_config",
		"-T", "-W", "localhost:22", "my-spark-mdns",
	}
	if !reflect.DeepEqual(gotArgv, wantArgv) {
		t.Fatalf("argv = %#v, want %#v", gotArgv, wantArgv)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("proxy command output = stdout %q, stderr %q; want empty", stdout.String(), stderr.String())
	}
}

func TestSSHRouteProxyRejectsUnsafeOrUnownedRouteBeforeExec(t *testing.T) {
	configPathErr := errors.New("config path unavailable")
	tests := []struct {
		name        string
		node        string
		alias       string
		config      string
		configErr   error
		wantErrPart string
	}{
		{
			name:        "hostile alias",
			node:        "-oProxyCommand=touch-bad",
			alias:       "-oProxyCommand=touch-bad",
			config:      activeRouteConfig,
			wantErrPart: "not canonical",
		},
		{
			name:        "node alias mismatch",
			node:        "my-spark-mdns",
			alias:       "other-spark",
			config:      activeRouteConfig,
			wantErrPart: "must match exactly",
		},
		{
			name:        "route absent",
			node:        "my-spark-mdns",
			alias:       "my-spark-mdns",
			config:      "Host unrelated\n  HostName unrelated.example\n",
			wantErrPart: "not active",
		},
		{
			name:  "duplicate ownership markers",
			node:  "my-spark-mdns",
			alias: "my-spark-mdns",
			config: activeRouteConfig + `# brev-external-node node_id=node-456 alias=my-spark-mdns
Host my-spark-mdns
  ProxyCommand none
`,
			wantErrPart: "not active",
		},
		{
			name:  "proxy neutralization missing",
			node:  "my-spark-mdns",
			alias: "my-spark-mdns",
			config: `# brev-external-node node_id=node-123 alias=my-spark-mdns
Host my-spark-mdns
  HostName tunnel.example
`,
			wantErrPart: "not active",
		},
		{
			name:        "config path unavailable",
			node:        "my-spark-mdns",
			alias:       "my-spark-mdns",
			config:      activeRouteConfig,
			configErr:   configPathErr,
			wantErrPart: configPathErr.Error(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			execCalled := false
			deps := routeDependencies{
				readConfig: func() (string, error) { return test.config, nil },
				configPath: func() (string, error) {
					if test.configErr != nil {
						return "", test.configErr
					}
					return "/Users/test/.brev/ssh_config", nil
				},
				execProxy: func(string, []string, []string) error {
					execCalled = true
					return nil
				},
			}
			cmd := newCmdNVSYNC(deps)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"ssh-route", "proxy", "--node=" + test.node, "--alias=" + test.alias})

			err := cmd.Execute()
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.wantErrPart)) {
				t.Fatalf("proxy error = %v, want containing %q", err, test.wantErrPart)
			}
			if execCalled {
				t.Fatal("proxy executor called for an unsafe or unowned route")
			}
		})
	}
}
