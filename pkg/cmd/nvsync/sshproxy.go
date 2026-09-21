package nvsync

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const systemSSHPath = "/usr/bin/ssh"

func newRouteProxyCommand(deps routeDependencies) *cobra.Command {
	var nodeName string
	var alias string
	cmd := &cobra.Command{
		Use:         "proxy",
		Hidden:      true,
		Args:        cobra.NoArgs,
		Annotations: machineAnnotations(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateRouteIdentity(nodeName, alias); err != nil {
				return err
			}
			config, err := deps.readConfig()
			if err != nil {
				return err
			}
			result := inspectRouteConfig(config, nodeName, alias)
			if result.SchemaVersion != routeSchemaVersion || result.NodeName != nodeName || result.SSHAlias != alias || result.State != "active" || result.Ownership != "brev" {
				return fmt.Errorf("Brev SSH route is not active")
			}
			configPath, err := deps.configPath()
			if err != nil {
				return err
			}
			argv := []string{systemSSHPath, "-F", configPath, "-T", "-W", "localhost:22", alias}
			return deps.execProxy(systemSSHPath, argv, os.Environ())
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "exact Brev external-node name")
	cmd.Flags().StringVar(&alias, "alias", "", "exact SSH alias owned by NVIDIA Sync")
	return cmd
}
