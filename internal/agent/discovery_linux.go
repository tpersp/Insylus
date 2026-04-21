package agent

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"insylus/internal/shared"
)

func collectTopologyDiscovery() shared.TopologyDiscovery {
	discovery := shared.TopologyDiscovery{
		DeviceType:    detectDeviceType(),
		Purpose:       shared.DevicePurposeUnknown,
		PlatformClass: detectPlatformClass(),
		UpdatedAt:     time.Now().UTC(),
	}
	discovery.WakeOnLAN = detectWakeOnLAN(discovery.DeviceType)
	discovery.Workloads = append(discovery.Workloads, discoverSystemdServices(&discovery.Warnings)...)
	discovery.Workloads = append(discovery.Workloads, discoverUserSystemdServices()...)
	if isDockerAvailable() {
		discovery.Workloads = append(discovery.Workloads, discoverDockerContainers(&discovery.Warnings)...)
		discovery.Purpose = shared.DevicePurposeDockerHost
	}
	if isProxmoxNode() {
		discovery.Purpose = shared.DevicePurposeProxmoxNode
		vms, lxcs := discoverProxmoxGuests(&discovery.Warnings)
		discovery.Workloads = append(discovery.Workloads, vms...)
		discovery.ChildCandidates = append(discovery.ChildCandidates, lxcs...)
	}
	return discovery
}

func detectWakeOnLAN(deviceType shared.DeviceType) shared.WakeOnLANInfo {
	if deviceType == shared.DeviceTypeContainer || deviceType == shared.DeviceTypeLXC {
		return shared.WakeOnLANInfo{Enabled: false, Reason: "not applicable for containers"}
	}
	if _, err := exec.LookPath("ethtool"); err != nil {
		return shared.WakeOnLANInfo{Enabled: false, Reason: "ethtool unavailable"}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return shared.WakeOnLANInfo{Enabled: false, Reason: "network interface discovery failed: " + err.Error()}
	}
	var best shared.WakeOnLANInfo
	for _, iface := range ifaces {
		if !isWakeCandidateInterface(iface) {
			continue
		}
		out, err := runOutput("ethtool", iface.Name)
		if err != nil {
			continue
		}
		info := parseEthtoolWakeOnLAN(out)
		info.MACAddress = strings.ToLower(iface.HardwareAddr.String())
		info.Interface = iface.Name
		info.Broadcast = interfaceBroadcast(iface)
		info.Port = 9
		info.LastDetected = time.Now().UTC()
		if !info.Supported {
			info.Reason = "magic packet wake is not supported"
		} else if !info.Active {
			info.Reason = "magic packet wake is supported but disabled"
		} else {
			info.Enabled = true
			info.Reason = "magic packet wake is enabled"
			return info
		}
		if best.Interface == "" {
			best = info
		}
	}
	if best.Interface != "" {
		return best
	}
	return shared.WakeOnLANInfo{Enabled: false, Reason: "no wakeable physical interface found"}
}

func isWakeCandidateInterface(iface net.Interface) bool {
	if iface.Flags&net.FlagLoopback != 0 {
		return false
	}
	if len(iface.HardwareAddr) != 6 {
		return false
	}
	if !isPreferredHostInterface(iface.Name) {
		return false
	}
	if isVirtualInterface(iface.Name) {
		return false
	}
	return true
}

func isVirtualInterface(name string) bool {
	target, err := os.Readlink(filepath.Join("/sys/class/net", name))
	if err != nil {
		return false
	}
	return strings.Contains(target, "/virtual/")
}

func parseEthtoolWakeOnLAN(raw string) shared.WakeOnLANInfo {
	info := shared.WakeOnLANInfo{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "supports wake-on":
			info.Supported = strings.Contains(value, "g")
		case "wake-on":
			info.Active = strings.Contains(value, "g")
		}
	}
	info.Enabled = info.Supported && info.Active
	return info
}

func interfaceBroadcast(iface net.Interface) string {
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ip, network, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil || network == nil {
			continue
		}
		mask := network.Mask
		if len(mask) != net.IPv4len {
			continue
		}
		broadcast := make(net.IP, net.IPv4len)
		for i := 0; i < net.IPv4len; i++ {
			broadcast[i] = ip4[i] | ^mask[i]
		}
		return broadcast.String()
	}
	return ""
}

func detectDeviceType() shared.DeviceType {
	if exists("/.dockerenv") {
		return shared.DeviceTypeContainer
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		text := string(data)
		if strings.Contains(text, "docker") || strings.Contains(text, "containerd") {
			return shared.DeviceTypeContainer
		}
		if strings.Contains(text, "lxc") {
			return shared.DeviceTypeLXC
		}
	}
	if out, err := runOutput("systemd-detect-virt"); err == nil {
		value := strings.TrimSpace(out)
		switch value {
		case "docker", "podman", "container-other":
			return shared.DeviceTypeContainer
		case "lxc":
			return shared.DeviceTypeLXC
		case "kvm", "qemu", "vmware", "microsoft", "oracle":
			return shared.DeviceTypeVM
		case "none", "":
			return shared.DeviceTypeBareMetal
		default:
			return shared.DeviceTypeVM
		}
	}
	return shared.DeviceTypeBareMetal
}

func detectPlatformClass() shared.PlatformClass {
	if data, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), "raspberry pi") {
			return shared.PlatformClassRaspberryPi
		}
	}
	if data, err := os.ReadFile("/sys/firmware/devicetree/base/model"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), "raspberry pi") {
			return shared.PlatformClassRaspberryPi
		}
	}
	return shared.PlatformClassGenericLinux
}

func discoverSystemdServices(warnings *[]string) []shared.Workload {
	out, err := runOutput("systemctl", "list-units", "--type=service", "--state=running", "--no-legend", "--no-pager")
	if err != nil {
		*warnings = append(*warnings, "systemd service discovery failed: "+err.Error())
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var workloads []shared.Workload
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if !strings.HasSuffix(name, ".service") {
			continue
		}
		trimmed := strings.TrimSuffix(name, ".service")
		if shouldIgnoreService(trimmed) {
			continue
		}
		workloads = append(workloads, shared.Workload{Name: trimmed, Kind: shared.WorkloadKindService, State: "running"})
	}
	sort.SliceStable(workloads, func(i, j int) bool {
		pi := serviceDiscoveryPriority(workloads[i].Name)
		pj := serviceDiscoveryPriority(workloads[j].Name)
		if pi != pj {
			return pi > pj
		}
		return workloads[i].Name < workloads[j].Name
	})
	if len(workloads) > 24 {
		workloads = workloads[:24]
	}
	return workloads
}

func discoverUserSystemdServices() []shared.Workload {
	runtimeDirs, err := filepath.Glob("/run/user/[0-9]*")
	if err != nil || len(runtimeDirs) == 0 {
		return nil
	}
	sort.Strings(runtimeDirs)

	var workloads []shared.Workload
	seen := map[string]struct{}{}
	for _, runtimeDir := range runtimeDirs {
		uid := filepath.Base(runtimeDir)
		if !isNumericID(uid) {
			continue
		}
		account, err := user.LookupId(uid)
		if err != nil || account.Username == "" {
			continue
		}
		userWorkloads := discoverUserSystemdServicesForUser(account.Username, runtimeDir)
		for _, workload := range userWorkloads {
			key := workload.Name + "\x00" + string(workload.Kind)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			workloads = append(workloads, workload)
			if len(workloads) >= 20 {
				return workloads
			}
		}
	}
	return workloads
}

func discoverUserSystemdServicesForUser(username, runtimeDir string) []shared.Workload {
	out, err := runOutput("runuser", "-u", username, "--", "env", "XDG_RUNTIME_DIR="+runtimeDir, "systemctl", "--user", "list-units", "--type=service", "--state=running", "--no-legend", "--no-pager")
	if err != nil {
		// User managers can disappear while we are collecting. Keep this best-effort and quiet unless debugging warnings already exist.
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var workloads []shared.Workload
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if !strings.HasSuffix(name, ".service") {
			continue
		}
		trimmed := strings.TrimSuffix(name, ".service")
		if shouldIgnoreUserService(trimmed) {
			continue
		}
		workloads = append(workloads, shared.Workload{
			Name:  username + "/" + trimmed,
			Kind:  shared.WorkloadKindService,
			State: "running",
		})
	}
	return workloads
}

func isDockerAvailable() bool {
	return exec.Command("docker", "info").Run() == nil
}

func discoverDockerContainers(warnings *[]string) []shared.Workload {
	out, err := runOutput("docker", "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		*warnings = append(*warnings, "docker discovery failed: "+err.Error())
		return nil
	}
	type dockerRow struct {
		Names  string `json:"Names"`
		Image  string `json:"Image"`
		State  string `json:"State"`
		Ports  string `json:"Ports"`
		Status string `json:"Status"`
	}
	var workloads []shared.Workload
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row dockerRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		workloads = append(workloads, shared.Workload{
			Name:      row.Names,
			Kind:      shared.WorkloadKindContainer,
			Image:     row.Image,
			State:     firstNonEmpty(row.State, row.Status),
			Endpoints: parseDockerPorts(row.Ports),
		})
	}
	return workloads
}

func parseDockerPorts(raw string) []shared.Endpoint {
	var endpoints []shared.Endpoint
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, "->") {
			continue
		}
		hostPart := strings.SplitN(part, "->", 2)[0]
		hostPart = strings.TrimSpace(hostPart)
		if idx := strings.LastIndex(hostPart, ":"); idx >= 0 && idx+1 < len(hostPart) {
			endpoints = append(endpoints, shared.Endpoint{
				Host: strings.TrimSpace(hostPart[:idx]),
				Port: strings.TrimSpace(hostPart[idx+1:]),
			})
		}
	}
	return endpoints
}

func isProxmoxNode() bool {
	if exists("/etc/pve") || exists("/usr/bin/pveversion") || exists("/usr/sbin/pveversion") {
		return true
	}
	_, qmErr := exec.LookPath("qm")
	_, pctErr := exec.LookPath("pct")
	return qmErr == nil || pctErr == nil
}

func discoverProxmoxGuests(warnings *[]string) ([]shared.Workload, []shared.ChildCandidate) {
	var workloads []shared.Workload
	var children []shared.ChildCandidate
	if out, err := runOutput("qm", "list"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || isProxmoxListHeader(fields) || !isNumericID(fields[0]) {
				continue
			}
			vmid := fields[0]
			name := fields[1]
			state := "unknown"
			if len(fields) >= 3 {
				state = fields[2]
			}
			workloads = append(workloads, shared.Workload{Name: name, Kind: shared.WorkloadKindVM, State: state})
			ips := discoverQemuGuestIPs(vmid)
			if len(ips) == 0 {
				ips = discoverQemuGuestIPsFromNeighborTable(vmid)
			}
			if len(ips) == 0 {
				ips = lookupIPsByName(name)
			}
			children = append(children, shared.ChildCandidate{Name: name, Kind: shared.WorkloadKindVM, IPs: ips})
		}
	} else {
		*warnings = append(*warnings, "proxmox vm discovery failed: "+err.Error())
	}
	if out, err := runOutput("pct", "list"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || isProxmoxListHeader(fields) || !isNumericID(fields[0]) {
				continue
			}
			ctid := fields[0]
			name := fields[len(fields)-1]
			state := "unknown"
			if len(fields) >= 2 {
				state = fields[1]
			}
			workloads = append(workloads, shared.Workload{Name: name, Kind: shared.WorkloadKindLXC, State: state})
			ips := discoverLXCGuestIPs(ctid)
			if len(ips) == 0 {
				ips = lookupIPsByName(name)
			}
			children = append(children, shared.ChildCandidate{Name: name, Kind: shared.WorkloadKindLXC, IPs: ips})
		}
	} else {
		*warnings = append(*warnings, "proxmox lxc discovery failed: "+err.Error())
	}
	return workloads, children
}

func isProxmoxListHeader(fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	first := strings.ToLower(strings.TrimSpace(fields[0]))
	if first == "vmid" || first == "id" {
		return true
	}
	if len(fields) > 1 && strings.EqualFold(fields[1], "name") {
		return true
	}
	return false
}

func isNumericID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func discoverQemuGuestIPs(vmid string) []string {
	out, err := runOutput("qm", "guest", "cmd", vmid, "network-get-interfaces")
	if err != nil {
		return nil
	}
	type guestIP struct {
		IPAddress string `json:"ip-address"`
		IPType    string `json:"ip-address-type"`
	}
	type guestIface struct {
		Name         string    `json:"name"`
		IPAddresses  []guestIP `json:"ip-addresses"`
		HardwareAddr string    `json:"hardware-address"`
	}
	var ifaces []guestIface
	if err := json.Unmarshal([]byte(out), &ifaces); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var ips []string
	for _, iface := range ifaces {
		for _, ip := range iface.IPAddresses {
			if ip.IPType != "ipv4" {
				continue
			}
			if strings.HasPrefix(ip.IPAddress, "127.") {
				continue
			}
			if _, ok := seen[ip.IPAddress]; ok {
				continue
			}
			seen[ip.IPAddress] = struct{}{}
			ips = append(ips, ip.IPAddress)
		}
	}
	sort.Strings(ips)
	return ips
}

func discoverQemuGuestIPsFromNeighborTable(vmid string) []string {
	config, err := runOutput("qm", "config", vmid)
	if err != nil {
		return nil
	}
	macs := parseQemuConfigMACs(config)
	if len(macs) == 0 {
		return nil
	}
	neighbors, err := runOutput("ip", "-4", "neigh", "show")
	if err != nil {
		return nil
	}
	return ipsForMACsFromNeighborTable(neighbors, macs)
}

func parseQemuConfigMACs(raw string) []string {
	seen := map[string]struct{}{}
	var macs []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "net") || !strings.Contains(line, ":") {
			continue
		}
		_, value, _ := strings.Cut(line, ":")
		for _, part := range strings.Split(value, ",") {
			_, mac, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			mac = strings.ToLower(strings.TrimSpace(mac))
			if !isMACAddress(mac) {
				continue
			}
			if _, ok := seen[mac]; ok {
				continue
			}
			seen[mac] = struct{}{}
			macs = append(macs, mac)
		}
	}
	sort.Strings(macs)
	return macs
}

func ipsForMACsFromNeighborTable(raw string, macs []string) []string {
	wanted := map[string]struct{}{}
	for _, mac := range macs {
		wanted[strings.ToLower(mac)] = struct{}{}
	}
	seen := map[string]struct{}{}
	var ips []string
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		ip := fields[0]
		if strings.Count(ip, ".") != 3 || strings.HasPrefix(ip, "127.") {
			continue
		}
		for i := 1; i+1 < len(fields); i++ {
			if fields[i] != "lladdr" {
				continue
			}
			mac := strings.ToLower(fields[i+1])
			if _, ok := wanted[mac]; !ok {
				continue
			}
			if _, ok := seen[ip]; ok {
				continue
			}
			seen[ip] = struct{}{}
			ips = append(ips, ip)
		}
	}
	sort.Strings(ips)
	return ips
}

func isMACAddress(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 6 {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 {
			return false
		}
		for _, r := range part {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func discoverLXCGuestIPs(ctid string) []string {
	out, err := runOutput("pct", "exec", ctid, "--", "hostname", "-I")
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var ips []string
	for _, field := range strings.Fields(out) {
		if strings.Count(field, ".") != 3 {
			continue
		}
		if strings.HasPrefix(field, "127.") {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		ips = append(ips, field)
	}
	sort.Strings(ips)
	return ips
}

func lookupIPsByName(name string) []string {
	ips, err := lookupIPv4(name)
	if err != nil {
		return nil
	}
	return ips
}

func shouldIgnoreService(name string) bool {
	if name == "" {
		return true
	}
	ignoreExact := map[string]struct{}{
		"accounts-daemon":     {},
		"avahi-daemon":        {},
		"bluetooth":           {},
		"chrony":              {},
		"containerd":          {},
		"controller":          {},
		"cron":                {},
		"dbus":                {},
		"dm-event":            {},
		"fwupd":               {},
		"getty@tty1":          {},
		"ksmtuned":            {},
		"lightdm":             {},
		"lxc-monitord":        {},
		"lxcfs":               {},
		"ModemManager":        {},
		"multipathd":          {},
		"NetworkManager":      {},
		"nfs-blkmap":          {},
		"polkit":              {},
		"postfix":             {},
		"pve-firewall":        {},
		"proxmox-firewall":    {},
		"pve-cluster":         {},
		"pve-lxc-syscalld":    {},
		"pvedaemon":           {},
		"pvefw-logger":        {},
		"pveproxy":            {},
		"pvescheduler":        {},
		"pvestatd":            {},
		"qmeventd":            {},
		"rpcbind":             {},
		"rrdcached":           {},
		"rsyslog":             {},
		"smartmontools":       {},
		"spiceproxy":          {},
		"ssh":                 {},
		"udisks2":             {},
		"unattended-upgrades": {},
		"upower":              {},
		"systemd-udevd":       {},
		"systemd-resolved":    {},
		"systemd-timesyncd":   {},
		"systemd-logind":      {},
		"systemd-networkd":    {},
		"systemd-journald":    {},
		"wpa_supplicant":      {},
		"x11vnc":              {},
		"zfs-zed":             {},
	}
	if _, ok := ignoreExact[name]; ok {
		return true
	}
	ignorePrefixes := []string{
		"getty@",
		"pve-container@",
		"serial-getty@",
		"systemd-",
		"user@",
	}
	for _, prefix := range ignorePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	// Keep clearly user-relevant services such as app services, Docker, qemu-guest-agent, and the Insylus agent itself.
	return false
}

func shouldIgnoreUserService(name string) bool {
	if shouldIgnoreService(name) {
		return true
	}
	ignoreExact := map[string]struct{}{
		"at-spi-dbus-bus":               {},
		"dbus":                          {},
		"dconf":                         {},
		"evolution-addressbook-factory": {},
		"evolution-calendar-factory":    {},
		"evolution-source-registry":     {},
		"gvfs-afc-volume-monitor":       {},
		"gvfs-daemon":                   {},
		"gvfs-goa-volume-monitor":       {},
		"gvfs-gphoto2-volume-monitor":   {},
		"gvfs-metadata":                 {},
		"gvfs-mtp-volume-monitor":       {},
		"gvfs-udisks2-volume-monitor":   {},
		"obex":                          {},
		"pipewire":                      {},
		"pipewire-pulse":                {},
		"pulseaudio":                    {},
		"ssh-agent":                     {},
		"wireplumber":                   {},
		"xdg-desktop-portal":            {},
		"xdg-desktop-portal-gnome":      {},
		"xdg-desktop-portal-gtk":        {},
		"xdg-document-portal":           {},
		"xdg-permission-store":          {},
		"xdg-user-dirs-update":          {},
	}
	if _, ok := ignoreExact[name]; ok {
		return true
	}
	ignorePrefixes := []string{
		"app-gnome-",
		"app-org.",
		"dbus-:",
		"flatpak-",
		"gvfs-",
		"snap.",
		"xdg-desktop-portal-",
	}
	for _, prefix := range ignorePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func serviceDiscoveryPriority(name string) int {
	switch {
	case isLocalSystemService(name):
		return 3
	case isExplicitlyRelevantService(name):
		return 2
	default:
		return 1
	}
}

func isLocalSystemService(name string) bool {
	if name == "" {
		return false
	}
	unitName := name
	if !strings.HasSuffix(unitName, ".service") {
		unitName += ".service"
	}
	for _, dir := range systemdLocalUnitDirs() {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, unitName)); err == nil {
			return true
		}
	}
	return false
}

func systemdLocalUnitDirs() []string {
	if raw := strings.TrimSpace(os.Getenv("INSYLUS_AGENT_SYSTEMD_LOCAL_UNIT_DIRS")); raw != "" {
		parts := strings.Split(raw, string(os.PathListSeparator))
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	}
	return []string{
		"/etc/systemd/system",
		"/usr/local/lib/systemd/system",
	}
}

func isExplicitlyRelevantService(name string) bool {
	switch strings.TrimSpace(name) {
	case "code-server", "docker", "echomosaic", "insylus-agent", "jellyfin", "pulse-agent", "qemu-guest-agent":
		return true
	default:
		return false
	}
}

func exists(path string) bool {
	if strings.Contains(path, "*") {
		matches, _ := filepath.Glob(path)
		return len(matches) > 0
	}
	_, err := os.Stat(path)
	return err == nil
}

func runOutput(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
