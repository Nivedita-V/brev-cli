package registration

import (
	"bytes"
	"errors"
	"testing"

	"github.com/brevdev/brev-cli/pkg/cmd/register"
	"github.com/stretchr/testify/require"
)

type stubStore struct {
	registration *register.DeviceRegistration
	err          error
	loads        int
}

func (s *stubStore) LoadAll() (*register.DeviceRegistration, error) {
	s.loads++
	return s.registration, s.err
}

func TestStatusJSONContract(t *testing.T) {
	tests := []struct {
		name  string
		store *stubStore
		want  string
	}{
		{
			name:  "not registered",
			store: &stubStore{err: register.ErrRegistrationNotFound},
			want:  `{"schema_version":1,"state":"not_registered"}` + "\n",
		},
		{
			name:  "pending",
			store: &stubStore{registration: &register.DeviceRegistration{Status: register.RegistrationStatusPending, DisplayName: "spark", OrgID: "org-1", OrgName: "Team"}},
			want:  `{"schema_version":1,"state":"pending","display_name":"spark","organization_id":"org-1","organization_name":"Team"}` + "\n",
		},
		{
			name:  "registered legacy",
			store: &stubStore{registration: &register.DeviceRegistration{ExternalNodeID: "node-1", DisplayName: "spark", OrgID: "org-1", RegisteredAt: "2026-09-14T12:00:00Z"}},
			want:  `{"schema_version":1,"state":"registered","external_node_id":"node-1","display_name":"spark","organization_id":"org-1","registered_at":"2026-09-14T12:00:00Z"}` + "\n",
		},
		{
			name:  "invalid",
			store: &stubStore{err: register.ErrRegistrationInvalid},
			want:  `{"schema_version":1,"state":"invalid","error_code":"registration_invalid"}` + "\n",
		},
		{
			name:  "unknown state",
			store: &stubStore{err: register.ErrRegistrationStatusUnknown},
			want:  `{"schema_version":1,"state":"invalid","error_code":"registration_status_unknown"}` + "\n",
		},
		{
			name:  "unreadable",
			store: &stubStore{err: errors.New("permission denied secret")},
			want:  `{"schema_version":1,"state":"unreadable","error_code":"registration_unreadable"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := NewCmdStatus(tt.store)
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			require.Equal(t, tt.want, out.String())
			require.Equal(t, 1, tt.store.loads)
		})
	}
}
