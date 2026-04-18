package agent

import (
	"testing"

	"insylus/internal/shared"
)

func TestManagedPolicyFromResponseUsesConfiguredGroups(t *testing.T) {
	got := managedPolicyFromResponse(shared.AgentPolicyResponse{
		ManagedUser:           "operator",
		ManagedGroups:         []string{"adm", "wheel"},
		SudoersPath:           "/tmp/sudoers",
		AuditReadmePath:       "/tmp/audit",
		AuthorizedKeysPath:    "/tmp/authorized_keys",
		ManagedAccountEnabled: true,
	})

	if got.User != "operator" {
		t.Fatalf("User = %q, want operator", got.User)
	}
	if len(got.Groups) != 2 || got.Groups[0] != "adm" || got.Groups[1] != "wheel" {
		t.Fatalf("Groups = %+v, want [adm wheel]", got.Groups)
	}
}

func TestManagedPolicyFromResponseDefaultsGroups(t *testing.T) {
	got := managedPolicyFromResponse(shared.AgentPolicyResponse{ManagedUser: "operator"})
	if len(got.Groups) != 1 || got.Groups[0] != "adm" {
		t.Fatalf("Groups = %+v, want default audit group [adm]", got.Groups)
	}
}
