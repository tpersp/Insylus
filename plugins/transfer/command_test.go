package transfer

import (
	"reflect"
	"testing"

	"insylus/internal/shared"
)

func TestParseEndpointRemote(t *testing.T) {
	got := parseEndpoint("docker01:/srv/media/file.txt")
	if !got.Remote || got.Device != "docker01" || got.Path != "/srv/media/file.txt" {
		t.Fatalf("parseEndpoint remote = %+v", got)
	}
}

func TestSCPArgsUsesBrokeredRemoteToRemoteCopy(t *testing.T) {
	src := endpoint{Remote: true, Device: "docker01", Path: "/src/file", ResolvedAs: "docker01"}
	dst := endpoint{Remote: true, Device: "animus", Path: "/dst/file", ResolvedAs: "animus"}
	got := scpArgs(src, dst, scpOptions{Recursive: true, Preserve: true, SSHOptions: []string{"ConnectTimeout=5"}})
	want := []string{"-3", "-r", "-p", "-o", "ConnectTimeout=5", "docker01:/src/file", "animus:/dst/file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scpArgs = %+v, want %+v", got, want)
	}
}

func TestRequireSSHReady(t *testing.T) {
	ready := shared.DeviceInventoryInfo{
		Connection: shared.DeviceConnection{SSHAlias: "docker01"},
		Access: shared.DeviceAccessInfo{
			DeviceMode:            shared.DeviceModeAccessManaged,
			ManagedAccountEnabled: true,
			AccessMode:            shared.AccessModeAudit,
			EnforcementSucceeded:  true,
		},
	}
	if err := requireSSHReady(ready); err != nil {
		t.Fatalf("ready device rejected: %v", err)
	}

	notReady := ready
	notReady.Access.EnforcementSucceeded = false
	if err := requireSSHReady(notReady); err == nil {
		t.Fatal("expected not-ready device to fail")
	}
}

func TestShellCommandQuotesDryRunOutput(t *testing.T) {
	got := shellCommand([]string{"scp", "-3", "docker01:/tmp/a file", "animus:/tmp/a file"})
	want := "scp -3 'docker01:/tmp/a file' 'animus:/tmp/a file'"
	if got != want {
		t.Fatalf("shellCommand = %q, want %q", got, want)
	}
}
