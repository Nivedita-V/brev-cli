// Package registration provides machine-readable local registration inspection.
package registration

import (
	"encoding/json"
	"errors"

	"github.com/brevdev/brev-cli/pkg/cmd/register"
	"github.com/spf13/cobra"
)

const schemaVersion = 1

type registrationStore interface {
	LoadAll() (*register.DeviceRegistration, error)
}

type statusOutput struct {
	SchemaVersion    int    `json:"schema_version"`
	State            string `json:"state"`
	ExternalNodeID   string `json:"external_node_id,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`
	RegisteredAt     string `json:"registered_at,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
}

// NewCmdRegistration returns the parent for local registration inspection commands.
func NewCmdRegistration(store registrationStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registration",
		Short: "Inspect local registered compute state",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(NewCmdStatus(store))
	return cmd
}

// NewCmdStatus returns a read-only command that emits the local registration state as JSON.
func NewCmdStatus(store registrationStore) *cobra.Command {
	return &cobra.Command{
		Use:         "status",
		Short:       "Print local registered compute state as JSON",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"machine-output": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			result := inspect(store)
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}

func inspect(store registrationStore) statusOutput {
	reg, err := store.LoadAll()
	if err != nil {
		switch {
		case errors.Is(err, register.ErrRegistrationNotFound):
			return statusOutput{SchemaVersion: schemaVersion, State: "not_registered"}
		case errors.Is(err, register.ErrRegistrationInvalid):
			return statusOutput{SchemaVersion: schemaVersion, State: "invalid", ErrorCode: "registration_invalid"}
		case errors.Is(err, register.ErrRegistrationStatusUnknown):
			return statusOutput{SchemaVersion: schemaVersion, State: "invalid", ErrorCode: "registration_status_unknown"}
		default:
			return statusOutput{SchemaVersion: schemaVersion, State: "unreadable", ErrorCode: "registration_unreadable"}
		}
	}

	state := "registered"
	if reg.Status == register.RegistrationStatusPending {
		state = "pending"
	}
	return statusOutput{
		SchemaVersion:    schemaVersion,
		State:            state,
		ExternalNodeID:   reg.ExternalNodeID,
		DisplayName:      reg.DisplayName,
		OrganizationID:   reg.OrgID,
		OrganizationName: reg.OrgName,
		RegisteredAt:     reg.RegisteredAt,
	}
}
