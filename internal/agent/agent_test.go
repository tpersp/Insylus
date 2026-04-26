package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoJSONCapsErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", maxErrorBodyBytes+1024), http.StatusInternalServerError)
	}))
	defer server.Close()

	runner := New(Config{ServerURL: server.URL})
	err := runner.doJSON(context.Background(), http.MethodGet, "/", "", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > maxErrorBodyBytes+256 {
		t.Fatalf("error too large: %d", len(err.Error()))
	}
}

func TestIsPreferredHostInterface(t *testing.T) {
	tests := []struct {
		name     string
		iface    string
		expected bool
	}{
		{name: "ethernet", iface: "eth0", expected: true},
		{name: "network manager bridge", iface: "enp1s0", expected: true},
		{name: "docker0", iface: "docker0", expected: false},
		{name: "docker bridge", iface: "br-a1b2c3", expected: false},
		{name: "veth", iface: "veth7b1d", expected: false},
		{name: "cni", iface: "cni0", expected: false},
		{name: "flannel", iface: "flannel.1", expected: false},
		{name: "libvirt", iface: "virbr0", expected: false},
		{name: "zerotier", iface: "ztabc123", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPreferredHostInterface(tt.iface); got != tt.expected {
				t.Fatalf("isPreferredHostInterface(%q) = %v, want %v", tt.iface, got, tt.expected)
			}
		})
	}
}

func TestShouldIgnoreService(t *testing.T) {
	ignored := []string{
		"accounts-daemon",
		"avahi-daemon",
		"bluetooth",
		"chrony",
		"containerd",
		"controller",
		"dm-event",
		"ksmtuned",
		"lightdm",
		"lxc-monitord",
		"lxcfs",
		"NetworkManager",
		"nfs-blkmap",
		"postfix",
		"pve-firewall",
		"proxmox-firewall",
		"pve-cluster",
		"pve-container@100",
		"pve-lxc-syscalld",
		"pvedaemon",
		"pvefw-logger",
		"pveproxy",
		"pvescheduler",
		"pvestatd",
		"qmeventd",
		"rrdcached",
		"smartmontools",
		"spiceproxy",
		"serial-getty@ttyS0",
		"ssh",
		"udisks2",
		"unattended-upgrades",
		"upower",
		"wpa_supplicant",
		"x11vnc",
		"zfs-zed",
	}
	for _, service := range ignored {
		t.Run("ignore "+service, func(t *testing.T) {
			if !shouldIgnoreService(service) {
				t.Fatalf("expected %q to be ignored", service)
			}
		})
	}

	kept := []string{"docker", "echomosaic", "insylus-agent", "qemu-guest-agent", "pulse-agent"}
	for _, service := range kept {
		t.Run("keep "+service, func(t *testing.T) {
			if shouldIgnoreService(service) {
				t.Fatalf("expected %q to be kept", service)
			}
		})
	}
}

func TestIsLocalSystemService(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INSYLUS_AGENT_SYSTEMD_LOCAL_UNIT_DIRS", dir)
	path := filepath.Join(dir, "echomosaic.service")
	if err := os.WriteFile(path, []byte("[Unit]\nDescription=Echo Mosaic\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !isLocalSystemService("echomosaic") {
		t.Fatal("expected echomosaic to be detected as a local system service")
	}
	if isLocalSystemService("ssh") {
		t.Fatal("expected ssh to be absent from local system service dirs")
	}
}

func TestServiceDiscoveryPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INSYLUS_AGENT_SYSTEMD_LOCAL_UNIT_DIRS", dir)
	if err := os.WriteFile(filepath.Join(dir, "echomosaic.service"), []byte("[Unit]\nDescription=Echo Mosaic\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, want := serviceDiscoveryPriority("echomosaic"), 3; got != want {
		t.Fatalf("serviceDiscoveryPriority(echomosaic) = %d, want %d", got, want)
	}
	if got, want := serviceDiscoveryPriority("docker"), 2; got != want {
		t.Fatalf("serviceDiscoveryPriority(docker) = %d, want %d", got, want)
	}
	if got, want := serviceDiscoveryPriority("custom-unmanaged"), 1; got != want {
		t.Fatalf("serviceDiscoveryPriority(custom-unmanaged) = %d, want %d", got, want)
	}
}

func TestShouldIgnoreUserService(t *testing.T) {
	ignored := []string{
		"dbus",
		"gvfs-daemon",
		"pipewire",
		"pipewire-pulse",
		"wireplumber",
		"xdg-desktop-portal",
		"app-gnome-keyring-secrets",
		"snap.snap-store.ubuntu-software",
	}
	for _, service := range ignored {
		t.Run("ignore "+service, func(t *testing.T) {
			if !shouldIgnoreUserService(service) {
				t.Fatalf("expected %q to be ignored", service)
			}
		})
	}

	kept := []string{"echomosaic", "code-server", "discord-bot", "picture-frame"}
	for _, service := range kept {
		t.Run("keep "+service, func(t *testing.T) {
			if shouldIgnoreUserService(service) {
				t.Fatalf("expected %q to be kept", service)
			}
		})
	}
}

func TestAppendIfMissing(t *testing.T) {
	values := []string{"10.10.10.31"}
	values = appendIfMissing(values, "10.10.10.31")
	values = appendIfMissing(values, "192.168.1.20")

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d: %+v", len(values), values)
	}
}

func TestParseQemuConfigMACs(t *testing.T) {
	raw := `
boot: order=scsi0;net0
net0: virtio=BC:24:11:AA:BB:CC,bridge=vmbr0,firewall=1
net1: e1000=DE:AD:BE:EF:00:01,bridge=vmbr1
scsi0: local-lvm:vm-100-disk-0,size=32G
`
	got := parseQemuConfigMACs(raw)
	want := []string{"bc:24:11:aa:bb:cc", "de:ad:be:ef:00:01"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("parseQemuConfigMACs() = %+v, want %+v", got, want)
	}
}

func TestParseEthtoolWakeOnLAN(t *testing.T) {
	raw := `
Settings for enp1s0:
	Supports Wake-on: pumbg
	Wake-on: g
`
	got := parseEthtoolWakeOnLAN(raw)
	if !got.Supported || !got.Active || !got.Enabled {
		t.Fatalf("expected enabled magic packet WOL, got %+v", got)
	}

	raw = `
Settings for enp1s0:
	Supports Wake-on: pumbg
	Wake-on: d
`
	got = parseEthtoolWakeOnLAN(raw)
	if !got.Supported || got.Active || got.Enabled {
		t.Fatalf("expected supported but disabled WOL, got %+v", got)
	}
}

func TestIPsForMACsFromNeighborTable(t *testing.T) {
	raw := `
10.10.10.22 dev vmbr0 lladdr bc:24:11:aa:bb:cc STALE
10.10.10.23 dev vmbr0 lladdr 00:11:22:33:44:55 REACHABLE
fe80::1 dev vmbr0 lladdr bc:24:11:aa:bb:cc router STALE
`
	got := ipsForMACsFromNeighborTable(raw, []string{"BC:24:11:AA:BB:CC"})
	want := []string{"10.10.10.22"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ipsForMACsFromNeighborTable() = %+v, want %+v", got, want)
	}
}

func TestIsProxmoxListHeader(t *testing.T) {
	if !isProxmoxListHeader([]string{"VMID", "NAME", "STATUS"}) {
		t.Fatal("expected qm header to be ignored")
	}
	if !isProxmoxListHeader([]string{"VMID", "Status", "Lock", "Name"}) {
		t.Fatal("expected pct header to be ignored")
	}
	if isProxmoxListHeader([]string{"101", "MiscServer", "running"}) {
		t.Fatal("expected vm row to be kept")
	}
	if isNumericID("config") {
		t.Fatal("expected non-row text to be ignored")
	}
	if !isNumericID("101") {
		t.Fatal("expected numeric vmid to be accepted")
	}
}

func TestCopyFileAtomicallyReplacesDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("write dst: %v", err)
	}

	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("expected replacement content, got %q", string(data))
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected mode 0755, got %o", info.Mode().Perm())
	}
}

func TestCopyFileCleansTemporaryFileOnSuccess(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".dst.tmp-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no temporary files, got %+v", matches)
	}
}

func TestInstallPathsFromEnvDefaults(t *testing.T) {
	got := installPathsFromEnv()
	if got.BinaryPath != "/usr/local/bin/insylus-agent" {
		t.Fatalf("BinaryPath = %q", got.BinaryPath)
	}
	if got.ConfigPath != "/etc/insylus-agent/config.json" {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
	if got.ServiceName != "insylus-agent.service" {
		t.Fatalf("ServiceName = %q", got.ServiceName)
	}
	if got.UnitPath != "/etc/systemd/system/insylus-agent.service" {
		t.Fatalf("UnitPath = %q", got.UnitPath)
	}
}

func TestInstallPathsFromEnvCustom(t *testing.T) {
	t.Setenv("INSYLUS_AGENT_BIN_PATH", "/custom/bin/agent")
	t.Setenv("INSYLUS_AGENT_CONFIG_PATH", "/custom/etc/config.json")
	t.Setenv("INSYLUS_AGENT_SERVICE_NAME", "custom-agent.service")
	t.Setenv("INSYLUS_AGENT_UNIT_PATH", "/custom/systemd/custom-agent.service")

	got := installPathsFromEnv()
	if got.BinaryPath != "/custom/bin/agent" {
		t.Fatalf("BinaryPath = %q", got.BinaryPath)
	}
	if got.ConfigPath != "/custom/etc/config.json" {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
	if got.ServiceName != "custom-agent.service" {
		t.Fatalf("ServiceName = %q", got.ServiceName)
	}
	if got.UnitPath != "/custom/systemd/custom-agent.service" {
		t.Fatalf("UnitPath = %q", got.UnitPath)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "0.1.0", b: "0.1.0", want: 0},
		{name: "newer patch", a: "0.1.0", b: "0.1.1", want: -1},
		{name: "older patch", a: "0.1.2", b: "0.1.1", want: 1},
		{name: "v prefix", a: "v0.1.0", b: "0.1.0", want: 0},
		{name: "missing patch", a: "0.1", b: "0.1.0", want: 0},
		{name: "malformed", a: "dev", b: "0.1.0", want: -1},
		{name: "empty", a: "", b: "0.1.0", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareVersions(tt.a, tt.b); got != tt.want {
				t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestManagedAuthorizedKeyBlockRoundTrip(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1 test"
	current := "ssh-ed25519 AAAAOTHER other\n"
	withBlock := upsertManagedAuthorizedKeyBlock(current, key)
	if !strings.Contains(withBlock, insylusKeyBlockBegin) || !strings.Contains(withBlock, key) {
		t.Fatalf("expected marked key block, got %q", withBlock)
	}
	next, changed, warning := removeManagedAuthorizedKeyContent(withBlock, key)
	if !changed || warning != "" {
		t.Fatalf("expected clean marked removal, changed=%v warning=%q next=%q", changed, warning, next)
	}
	if strings.TrimSpace(next) != strings.TrimSpace(current) {
		t.Fatalf("expected non-insylus key to remain, got %q", next)
	}
}

func TestRemoveLegacyExactAuthorizedKey(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1 test"
	next, changed, warning := removeManagedAuthorizedKeyContent(key+"\n", key)
	if !changed || warning != "" || strings.TrimSpace(next) != "" {
		t.Fatalf("expected exact legacy key removal, changed=%v warning=%q next=%q", changed, warning, next)
	}
}

func TestLeaveMixedUnmarkedAuthorizedKeysUntouched(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1 test"
	current := key + "\nssh-ed25519 AAAAOTHER other\n"
	next, changed, warning := removeManagedAuthorizedKeyContent(current, key)
	if changed || warning == "" || next != current {
		t.Fatalf("expected mixed unmarked content untouched with warning, changed=%v warning=%q next=%q", changed, warning, next)
	}
}

func TestSystemdStatusExitCodeExistsHandling(t *testing.T) {
	tests := []struct {
		name       string
		code       int
		wantExists bool
		wantMiss   bool
	}{
		{name: "failed unit still exists", code: 1, wantExists: true},
		{name: "inactive unit still exists", code: 3, wantExists: true},
		{name: "missing unit", code: 4, wantMiss: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runExitCode(t, tt.code)
			if got := systemdStatusExitCodeMeansExists(err); got != tt.wantExists {
				t.Fatalf("exists(%d) = %v, want %v", tt.code, got, tt.wantExists)
			}
			if got := systemdStatusExitCodeMeansMissing(err); got != tt.wantMiss {
				t.Fatalf("missing(%d) = %v, want %v", tt.code, got, tt.wantMiss)
			}
		})
	}
}

func TestSystemdActiveExitCodeMeansInactive(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		wantInactive bool
	}{
		{name: "generic inactive", code: 1, wantInactive: true},
		{name: "inactive", code: 3, wantInactive: true},
		{name: "missing", code: 4, wantInactive: true},
		{name: "other error", code: 5, wantInactive: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runExitCode(t, tt.code)
			if got := systemdActiveExitCodeMeansInactive(err); got != tt.wantInactive {
				t.Fatalf("inactive(%d) = %v, want %v", tt.code, got, tt.wantInactive)
			}
		})
	}
}

func runExitCode(t *testing.T, code int) error {
	t.Helper()
	cmd := exec.Command("bash", "-lc", fmt.Sprintf("exit %d", code))
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected exit code %d command to fail", code)
	}
	return err
}
