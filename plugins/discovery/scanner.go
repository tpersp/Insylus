package discovery

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxScanHosts   = 4096
	scanWorkers    = 64
	portTimeout    = 250 * time.Millisecond
	lookupTimeout  = 400 * time.Millisecond
	defaultTimeout = 45 * time.Second
)

var defaultPorts = []int{22, 80, 443, 445, 8006, 8080, 8443}

type scanner interface {
	ScanSubnet(ctx context.Context, cidr string, ports []int) (scanResponse, error)
}

type lanScanner struct{}

func (lanScanner) ScanSubnet(ctx context.Context, cidr string, ports []int) (scanResponse, error) {
	cidr = strings.TrimSpace(cidr)
	ports = normalizedPorts(ports)
	ips, err := hostsFromCIDR(cidr)
	if err != nil {
		return scanResponse{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	type outcome struct {
		result scanResult
		ok     bool
	}
	work := make(chan string)
	results := make(chan outcome, len(ips))

	var wg sync.WaitGroup
	workerCount := scanWorkers
	if len(ips) < workerCount {
		workerCount = len(ips)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range work {
				result, ok := probeHost(ctx, ip, ports)
				results <- outcome{result: result, ok: ok}
			}
		}()
	}

	go func() {
		defer close(work)
		for _, ip := range ips {
			select {
			case <-ctx.Done():
				return
			case work <- ip:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	candidates := make([]scanResult, 0)
	for outcome := range results {
		if outcome.ok {
			candidates = append(candidates, outcome.result)
		}
	}

	arp := arpTable()
	for i := range candidates {
		if candidates[i].MACAddress == "" {
			candidates[i].MACAddress = arp[candidates[i].IPAddress]
		}
		if candidates[i].DisplayName == "" {
			candidates[i].DisplayName = suggestedName(candidates[i].Hostname, candidates[i].IPAddress)
		}
		candidates[i].KindHint = kindHintForPorts(candidates[i].OpenPorts)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return compareIPs(candidates[i].IPAddress, candidates[j].IPAddress) < 0
	})

	return scanResponse{
		CIDR:       cidr,
		Ports:      ports,
		Scanned:    len(ips),
		Discovered: len(candidates),
	}, nil
}

func normalizedPorts(ports []int) []int {
	if len(ports) == 0 {
		ports = defaultPorts
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		out = append(out, port)
	}
	sort.Ints(out)
	if len(out) == 0 {
		return append([]int(nil), defaultPorts...)
	}
	return out
}

func hostsFromCIDR(cidr string) ([]string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet: %w", err)
	}
	if !prefix.Addr().Is4() {
		return nil, fmt.Errorf("only IPv4 subnets are supported")
	}
	prefix = prefix.Masked()
	ones, bits := prefix.Bits(), prefix.Addr().BitLen()
	if bits != 32 {
		return nil, fmt.Errorf("only IPv4 subnets are supported")
	}
	hostBits := bits - ones
	total := 1 << hostBits
	if total > maxScanHosts {
		return nil, fmt.Errorf("subnet too large: %s has %d addresses, max is %d", cidr, total, maxScanHosts)
	}

	start := ipv4ToUint32(prefix.Addr())
	end := start + uint32(total) - 1
	if total > 2 && ones < 31 {
		start++
		end--
	}

	out := make([]string, 0, max(1, int(end-start+1)))
	for value := start; value <= end; value++ {
		out = append(out, uint32ToIPv4(value).String())
	}
	return out, nil
}

func probeHost(ctx context.Context, ip string, ports []int) (scanResult, bool) {
	result := scanResult{IPAddress: ip}
	alive := false
	for _, port := range ports {
		dialer := net.Dialer{Timeout: portTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
		if err == nil {
			alive = true
			result.OpenPorts = append(result.OpenPorts, port)
			_ = conn.Close()
			continue
		}
		if isConnectionRefused(err) {
			alive = true
		}
	}
	if !alive {
		return scanResult{}, false
	}

	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(lookupCtx, ip)
	if err == nil && len(names) > 0 {
		result.Hostname = strings.TrimSuffix(strings.TrimSpace(names[0]), ".")
	}
	result.DisplayName = suggestedName(result.Hostname, ip)
	return result, true
}

func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

func arpTable() map[string]string {
	file, err := os.Open("/proc/net/arp")
	if err != nil {
		return map[string]string{}
	}
	defer file.Close()

	out := map[string]string{}
	scanner := bufio.NewScanner(file)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		ip := strings.TrimSpace(fields[0])
		mac := strings.ToLower(strings.TrimSpace(fields[3]))
		if ip == "" || mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		out[ip] = mac
	}
	return out
}

func suggestedName(hostname, ip string) string {
	base := strings.TrimSpace(hostname)
	if base != "" {
		if host, _, err := net.SplitHostPort(base); err == nil {
			base = host
		}
		base = strings.TrimSuffix(base, ".local")
		base = strings.TrimSuffix(base, ".lan")
		if idx := strings.IndexByte(base, '.'); idx > 0 {
			base = base[:idx]
		}
		base = sanitizeName(base)
	}
	if base != "" {
		return base
	}
	return "device-" + strings.ReplaceAll(ip, ".", "-")
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.' || r == ' ':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func kindHintForPorts(ports []int) string {
	set := map[int]struct{}{}
	for _, port := range ports {
		set[port] = struct{}{}
	}
	switch {
	case hasPort(set, 8006):
		return "proxmox-node"
	case hasPort(set, 2375) || hasPort(set, 2376):
		return "docker-host"
	case hasPort(set, 22):
		return "linux-host"
	default:
		return "target"
	}
}

func hasPort(ports map[int]struct{}, port int) bool {
	_, ok := ports[port]
	return ok
}

func compareIPs(a, b string) int {
	ap, aErr := netip.ParseAddr(a)
	bp, bErr := netip.ParseAddr(b)
	if aErr != nil || bErr != nil {
		return strings.Compare(a, b)
	}
	return ap.Compare(bp)
}

func ipv4ToUint32(addr netip.Addr) uint32 {
	raw := addr.As4()
	return uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
}

func uint32ToIPv4(value uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{
		byte(value >> 24),
		byte(value >> 16),
		byte(value >> 8),
		byte(value),
	})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
