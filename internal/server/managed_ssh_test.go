package server

import (
	"strings"
	"testing"

	"insylus/internal/shared"
)

func TestRenderManagedSSHConfigUsesFriendlyAndLowercaseAlias(t *testing.T) {
	records := []DeviceRecord{
		{
			Device: shared.Device{Name: "MiscServer", IPs: []string{"10.10.10.22"}},
			Policy: shared.Policy{DeviceMode: shared.DeviceModeAccessManaged, ManagedAccountEnabled: true, AccessMode: shared.AccessModeSudoPasswordless},
		},
	}
	got := renderManagedSSHConfig(records, "insylus", "/home/insylus/.ssh/id_ed25519", "/etc/ssh/ssh_known_hosts_insylus")
	wantParts := []string{
		"Host MiscServer miscserver",
		"HostName 10.10.10.22",
		"User insylus",
		"IdentityFile /home/insylus/.ssh/id_ed25519",
		"StrictHostKeyChecking accept-new",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("expected config to contain %q, got:\n%s", part, got)
		}
	}
}
