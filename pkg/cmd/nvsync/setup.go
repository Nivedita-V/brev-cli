package nvsync

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/brevdev/brev-cli/pkg/cmd/enablessh"
	"github.com/brevdev/brev-cli/pkg/cmd/grantssh"
	"github.com/brevdev/brev-cli/pkg/cmd/register"
	"github.com/brevdev/brev-cli/pkg/terminal"
	"github.com/spf13/cobra"
)

const (
	setupRegisterExitCode       = 20
	setupEnableSSHExitCode      = 21
	setupGrantSSHExitCode       = 22
	setupConfigureRouteExitCode = 23
)

// setupStore combines the Brev-owned capabilities used by the two interactive
// NVIDIA Sync setup sequences.
type setupStore interface {
	register.RegisterStore
	enablessh.EnableSSHStore
	grantssh.GrantSSHStore
	routeStore
}

// setupStage runs one existing Brev command inside the current process so all
// stages on the same machine share its ephemeral authentication session.
type setupStage func(context.Context, io.Reader, io.Writer, io.Writer, []string) error

// setupDependencies contains the existing Brev operations composed by the
// NVIDIA Sync setup commands.
type setupDependencies struct {
	register       setupStage
	enableSSH      setupStage
	grantSSH       setupStage
	configureRoute setupStage
}

// setupStageError preserves the failed setup stage as a stable process exit
// code without parsing or capturing interactive terminal output.
type setupStageError struct {
	code int
	err  error
}

func (err setupStageError) Error() string {
	return err.err.Error()
}

func (err setupStageError) Unwrap() error {
	return err.err
}

// ExitCode returns the stable code consumed by NVIDIA Sync's phase mapping.
func (err setupStageError) ExitCode() int {
	return err.code
}

func newSetupDependencies(t *terminal.Terminal, store setupStore) setupDependencies {
	return setupDependencies{
		register: embeddedCommandStage(func() *cobra.Command {
			return register.NewCmdRegister(t, store)
		}),
		enableSSH: embeddedCommandStage(func() *cobra.Command {
			return enablessh.NewCmdEnableSSH(t, store)
		}),
		grantSSH: embeddedCommandStage(func() *cobra.Command {
			return grantssh.NewCmdGrantSSH(t, store)
		}),
		configureRoute: embeddedCommandStage(func() *cobra.Command {
			return newCmdNVSYNC(newRouteDependencies(store))
		}),
	}
}

func embeddedCommandStage(factory func() *cobra.Command) setupStage {
	return func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
		cmd := factory()
		cmd.SetIn(stdin)
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		if args == nil {
			args = []string{}
		}
		cmd.SetArgs(args)
		return cmd.ExecuteContext(ctx)
	}
}

func newSetupTargetCommand(deps setupDependencies) *cobra.Command {
	var alias string
	cmd := &cobra.Command{
		Use:         "setup-target",
		Hidden:      true,
		Args:        cobra.NoArgs,
		Annotations: machineAnnotations(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateRouteIdentity(alias, alias); err != nil {
				return err
			}
			if err := deps.register(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), []string{"--fixed-name", alias}); err != nil {
				return setupStageError{code: setupRegisterExitCode, err: err}
			}
			if err := deps.enableSSH(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), nil); err != nil {
				return setupStageError{code: setupEnableSSHExitCode, err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "exact NVIDIA Sync SSH alias and Brev node name")
	return cmd
}

func newSetupHostCommand(deps setupDependencies) *cobra.Command {
	var nodeName string
	var alias string
	var linuxUser string
	cmd := &cobra.Command{
		Use:         "setup-host",
		Hidden:      true,
		Args:        cobra.NoArgs,
		Annotations: machineAnnotations(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateRouteIdentity(nodeName, alias); err != nil {
				return err
			}
			if strings.TrimSpace(linuxUser) == "" {
				return fmt.Errorf("--linux-user is required")
			}
			grantArgs := []string{"--interactive", "--self", "--node", nodeName, "--linux-user", linuxUser}
			if err := deps.grantSSH(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), grantArgs); err != nil {
				return setupStageError{code: setupGrantSSHExitCode, err: err}
			}
			routeArgs := []string{"ssh-route", "configure", "--node", nodeName, "--alias", alias}
			if err := deps.configureRoute(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), routeArgs); err != nil {
				return setupStageError{code: setupConfigureRouteExitCode, err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "exact Brev external-node name")
	cmd.Flags().StringVar(&alias, "alias", "", "exact NVIDIA Sync SSH alias")
	cmd.Flags().StringVar(&linuxUser, "linux-user", "", "Linux user granted access on the target")
	return cmd
}
