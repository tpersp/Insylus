package wake

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

var ErrWakeOnLANUnavailable = errors.New("wake-on-lan is not available for this device")

func WakeDevice(ctx context.Context, device pluginhost.InventoryDevice) (Result, error) {
	wol := device.WakeOnLAN
	if !wol.Enabled {
		if wol.Reason != "" {
			return Result{}, fmt.Errorf("%w: %s", ErrWakeOnLANUnavailable, wol.Reason)
		}
		return Result{}, ErrWakeOnLANUnavailable
	}
	if deviceIsOnline(device.LastSeenAt) {
		return Result{
			DeviceID: device.ID,
			Name:     device.Name,
			Status:   "already_online",
			Message:  "device is already online",
		}, nil
	}
	hw, err := net.ParseMAC(wol.MACAddress)
	if err != nil || len(hw) != 6 {
		return Result{}, fmt.Errorf("invalid wake-on-lan mac address %q", wol.MACAddress)
	}
	packet := magicPacket(hw)
	targets := wakeTargets(wol, device.IPs)
	if len(targets) == 0 {
		targets = []string{"255.255.255.255:9"}
	}
	var sent []string
	var errs []string
	for _, target := range targets {
		if err := sendWakePacket(ctx, target, packet); err != nil {
			errs = append(errs, target+": "+err.Error())
			continue
		}
		sent = append(sent, target)
	}
	if len(sent) == 0 {
		return Result{}, fmt.Errorf("wake-on-lan send failed: %s", strings.Join(errs, "; "))
	}
	return Result{
		DeviceID:   device.ID,
		Name:       device.Name,
		MACAddress: strings.ToLower(wol.MACAddress),
		Targets:    sent,
		Status:     "sent",
		Message:    "wake command accepted; magic packet sent",
	}, nil
}

func deviceIsOnline(lastSeen time.Time) bool {
	return !lastSeen.IsZero() && time.Since(lastSeen) <= shared.DeviceOnlineWindow
}

func magicPacket(hw net.HardwareAddr) []byte {
	packet := make([]byte, 6+16*6)
	for i := 0; i < 6; i++ {
		packet[i] = 0xff
	}
	offset := 6
	for i := 0; i < 16; i++ {
		copy(packet[offset:offset+6], hw)
		offset += 6
	}
	return packet
}

func wakeTargets(wol shared.WakeOnLANInfo, ips []string) []string {
	port := wol.Port
	if port == 0 {
		port = 9
	}
	seen := map[string]struct{}{}
	var targets []string
	add := func(host string) {
		host = strings.TrimSpace(host)
		if host == "" {
			return
		}
		target := net.JoinHostPort(host, fmt.Sprint(port))
		if _, ok := seen[target]; ok {
			return
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	add(wol.Broadcast)
	for _, ip := range ips {
		add(ipv4Slash24Broadcast(ip))
	}
	add("255.255.255.255")
	return targets
}

func ipv4Slash24Broadcast(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw)).To4()
	if ip == nil {
		return ""
	}
	ip[3] = 255
	return ip.String()
}

func sendWakePacket(ctx context.Context, target string, packet []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", target)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(packet)
	return err
}
