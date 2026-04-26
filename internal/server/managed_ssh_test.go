package server

import (
	"context"
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

func TestResolveManagedSSHOptionsUsesPersistedManagedUser(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	if err := store.SetManagedAccountConfig(context.Background(), shared.ManagedAccountConfig{
		ManagedUser: "aia",
		AccessMode:  shared.AccessModeAudit,
	}); err != nil {
		t.Fatalf("SetManagedAccountConfig: %v", err)
	}

	opts, err := resolveManagedSSHOptions(context.Background(), store, ManagedSSHSyncOptions{
		SSHUser:      "bob",
		IdentityFile: "/home/bob/.ssh/id_ed25519",
	})
	if err != nil {
		t.Fatalf("resolveManagedSSHOptions: %v", err)
	}
	if opts.SSHUser != "aia" {
		t.Fatalf("SSHUser = %q, want aia", opts.SSHUser)
	}
	if opts.IdentityFile != "/home/aia/.ssh/id_ed25519" {
		t.Fatalf("IdentityFile = %q, want /home/aia/.ssh/id_ed25519", opts.IdentityFile)
	}
}

func TestResolveManagedSSHOptionsKeepsExplicitIdentityFile(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	if err := store.SetManagedAccountConfig(context.Background(), shared.ManagedAccountConfig{
		ManagedUser: "aia",
		AccessMode:  shared.AccessModeAudit,
	}); err != nil {
		t.Fatalf("SetManagedAccountConfig: %v", err)
	}

	opts, err := resolveManagedSSHOptions(context.Background(), store, ManagedSSHSyncOptions{
		SSHUser:      "bob",
		IdentityFile: "/etc/insylus/controller_key",
	})
	if err != nil {
		t.Fatalf("resolveManagedSSHOptions: %v", err)
	}
	if opts.SSHUser != "aia" {
		t.Fatalf("SSHUser = %q, want aia", opts.SSHUser)
	}
	if opts.IdentityFile != "/etc/insylus/controller_key" {
		t.Fatalf("IdentityFile = %q, want explicit identity path", opts.IdentityFile)
	}
}
