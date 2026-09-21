// Package nvsync exposes hidden machine-readable integration commands for
// NVIDIA Sync without exposing Brev's private configuration details.
package nvsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/brevdev/brev-cli/pkg/cmd/refresh"
	"github.com/brevdev/brev-cli/pkg/cmd/util"
	"github.com/brevdev/brev-cli/pkg/ssh"
	"github.com/spf13/cobra"
)

const routeSchemaVersion = 1

var errRouteVerification = errors.New("Brev SSH route verification failed")

// RouteResult is the stable machine-readable ownership result consumed by
// NVIDIA Sync.
type RouteResult struct {
	SchemaVersion int    `json:"schema_version"`
	NodeName      string `json:"node_name"`
	SSHAlias      string `json:"ssh_alias"`
	State         string `json:"state"`
	Ownership     string `json:"ownership,omitempty"`
}

type routeStore interface {
	refresh.RefreshStore
}

type routeDependencies struct {
	prepare    func(context.Context, string, string) error
	refresh    func() error
	readConfig func() (string, error)
	configPath func() (string, error)
	execProxy  func(string, []string, []string) error
}

// NewCmdNVSYNC returns Brev's hidden NVIDIA Sync integration command tree.
func NewCmdNVSYNC(store routeStore) *cobra.Command {
	return newCmdNVSYNC(newRouteDependencies(store))
}

func newRouteDependencies(store routeStore) routeDependencies {
	return routeDependencies{
		prepare: func(ctx context.Context, nodeName, alias string) error {
			node, err := util.FindExternalNodeWithContext(ctx, store, nodeName)
			if err != nil {
				return err
			}
			if node == nil || node.GetName() != nodeName {
				return fmt.Errorf("no exact external node found with name %q", nodeName)
			}
			info, err := util.ResolveExternalNodeSSH(store, node)
			if err != nil {
				return err
			}
			if info.SSHAlias() != alias {
				return fmt.Errorf("node %q resolves to SSH alias %q, not %q", nodeName, info.SSHAlias(), alias)
			}
			return nil
		},
		refresh: func() error { return refresh.RunRefresh(store) },
		readConfig: func() (string, error) {
			path, err := store.GetBrevSSHConfigPath()
			if err != nil {
				return "", err
			}
			exists, err := store.FileExists(path)
			if err != nil {
				return "", err
			}
			if !exists {
				return "", nil
			}
			return store.GetFileAsString(path)
		},
		configPath: store.GetBrevSSHConfigPath,
		execProxy:  execProxyProcess,
	}
}

func newCmdNVSYNC(deps routeDependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "nvsync",
		Hidden: true,
		Args:   cobra.NoArgs,
	}
	sshRoute := &cobra.Command{
		Use:         "ssh-route",
		Hidden:      true,
		Args:        cobra.NoArgs,
		Annotations: machineAnnotations(),
	}
	sshRoute.AddCommand(newRouteActionCommand("configure", deps))
	sshRoute.AddCommand(newRouteActionCommand("status", deps))
	sshRoute.AddCommand(newRouteActionCommand("remove", deps))
	sshRoute.AddCommand(newRouteProxyCommand(deps))
	cmd.AddCommand(sshRoute)
	return cmd
}

func newRouteActionCommand(action string, deps routeDependencies) *cobra.Command {
	var nodeName string
	var alias string
	cmd := &cobra.Command{
		Use:         action,
		Hidden:      true,
		Args:        cobra.NoArgs,
		Annotations: machineAnnotations(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateRouteIdentity(nodeName, alias); err != nil {
				return err
			}
			result, err := runRouteAction(cmd.Context(), action, nodeName, alias, deps)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "exact Brev external-node name")
	cmd.Flags().StringVar(&alias, "alias", "", "exact SSH alias owned by NVIDIA Sync")
	return cmd
}

func machineAnnotations() map[string]string {
	return map[string]string{"machine-output": "true"}
}

func validateRouteIdentity(nodeName, alias string) error {
	if nodeName == "" || alias == "" {
		return fmt.Errorf("--node and --alias are required")
	}
	if nodeName != alias {
		return fmt.Errorf("Brev node name and SSH alias must match exactly")
	}
	if ssh.SanitizeNodeName(alias) != alias {
		return fmt.Errorf("SSH alias %q is not canonical", alias)
	}
	return nil
}

func runRouteAction(ctx context.Context, action, nodeName, alias string, deps routeDependencies) (RouteResult, error) {
	switch action {
	case "configure":
		if err := deps.prepare(ctx, nodeName, alias); err != nil {
			return RouteResult{}, err
		}
		if err := deps.refresh(); err != nil {
			return RouteResult{}, err
		}
	case "remove":
		if err := deps.refresh(); err != nil {
			return RouteResult{}, err
		}
	case "status":
	default:
		return RouteResult{}, fmt.Errorf("unsupported route action %q", action)
	}

	config, err := deps.readConfig()
	if err != nil {
		return RouteResult{}, err
	}
	result := inspectRouteConfig(config, nodeName, alias)
	if action == "configure" && result.State != "active" {
		return RouteResult{}, fmt.Errorf("%w: configured route is %s", errRouteVerification, result.State)
	}
	if action == "remove" && result.State != "absent" {
		return RouteResult{}, fmt.Errorf("%w: removed route is %s", errRouteVerification, result.State)
	}
	return result, nil
}

func inspectRouteConfig(config, nodeName, alias string) RouteResult {
	result := RouteResult{
		SchemaVersion: routeSchemaVersion,
		NodeName:      nodeName,
		SSHAlias:      alias,
		State:         "absent",
	}
	lines := strings.Split(config, "\n")
	markerIndexes := make([]int, 0, 1)
	invalidMarker := false
	markerPrefix := "# brev-external-node "
	for i, line := range lines {
		if !strings.HasPrefix(line, markerPrefix) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, markerPrefix))
		markerAlias := ""
		nodeID := ""
		for _, field := range fields {
			if strings.HasPrefix(field, "alias=") {
				markerAlias = strings.TrimPrefix(field, "alias=")
			}
			if strings.HasPrefix(field, "node_id=") {
				nodeID = strings.TrimPrefix(field, "node_id=")
			}
		}
		if markerAlias == alias {
			markerIndexes = append(markerIndexes, i)
			invalidMarker = invalidMarker || nodeID == ""
		}
	}
	if len(markerIndexes) == 0 {
		return result
	}
	result.Ownership = "brev"
	if len(markerIndexes) != 1 || invalidMarker {
		result.State = "invalid"
		return result
	}

	start := markerIndexes[0] + 1
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], markerPrefix) {
			end = i
			break
		}
	}
	hostLine := "Host " + alias
	for i := start; i < end; i++ {
		if strings.TrimSpace(lines[i]) != hostLine {
			continue
		}
		for j := i + 1; j < end; j++ {
			trimmed := strings.TrimSpace(lines[j])
			if strings.HasPrefix(trimmed, "Host ") || strings.HasPrefix(trimmed, "Match ") {
				break
			}
			if strings.EqualFold(trimmed, "ProxyCommand none") {
				result.State = "active"
				return result
			}
		}
	}
	result.State = "invalid"
	return result
}
