package discovery

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxScanHosts   = 4096
	scanWorkers    = 64
	lookupTimeout  = 400 * time.Millisecond
	defaultTimeout = 45 * time.Second
)

type scanner interface {
	ScanSubnet(ctx context.Context, cidr string) (scanResponse, error)
}

type lanScanner struct{}

func (lanScanner) ScanSubnet(ctx context.Context, cidr string) (scanResponse, error) {
	cidr = strings.TrimSpace(cidr)
	ips, err := hostsFromCIDR(cidr)
	if err != nil {
		return scanResponse{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	type outcome struct {
		ip      string
		replied bool
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
				results <- outcome{ip: ip, replied: pingHost(ctx, ip)}
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

	pingReplies := make(map[string]bool, len(ips))
	for outcome := range results {
		pingReplies[outcome.ip] = outcome.replied
	}

	arp := arpTable()
	candidates := make([]scanResult, 0, len(ips))
	for _, ip := range ips {
		mac := arp[ip]
		if !pingReplies[ip] && mac == "" {
			continue
		}
		result := scanResult{
			IPAddress:  ip,
			MACAddress: mac,
			KindHint:   "linux-host",
		}
		lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
		names, err := net.DefaultResolver.LookupAddr(lookupCtx, ip)
		cancel()
		if err == nil && len(names) > 0 {
			result.Hostname = strings.TrimSuffix(strings.TrimSpace(names[0]), ".")
		}
		result.DisplayName = suggestedName(result.Hostname, ip)
		candidates = append(candidates, result)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return compareIPs(candidates[i].IPAddress, candidates[j].IPAddress) < 0
	})

	return scanResponse{
		CIDR:       cidr,
		Scanned:    len(ips),
		Discovered: len(candidates),
		Candidates: flattenScanResults(candidates),
	}, nil
}

func flattenScanResults(items []scanResult) []candidate {
	out := make([]candidate, 0, len(items))
	for _, item := range items {
		out = append(out, candidate{
			DisplayName: item.DisplayName,
			Hostname:    item.Hostname,
			IPAddress:   item.IPAddress,
			MACAddress:  item.MACAddress,
			OpenPorts:   append([]int(nil), item.OpenPorts...),
			KindHint:    item.KindHint,
			Status:      statusPending,
		})
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

func pingHost(ctx context.Context, ip string) bool {
	cmd := exec.CommandContext(ctx, "ping", "-n", "-c", "1", "-W", "1", ip)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
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
	return ip
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
		case r == '.':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
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
