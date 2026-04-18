package wake

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

func TestMagicPacket(t *testing.T) {
	hw, err := net.ParseMAC("00:11:22:33:44:55")
	if err != nil {
		t.Fatalf("ParseMAC: %v", err)
	}
	packet := magicPacket(hw)
	if len(packet) != 102 {
		t.Fatalf("expected 102-byte magic packet, got %d", len(packet))
	}
	for i := 0; i < 6; i++ {
		if packet[i] != 0xff {
			t.Fatalf("expected sync stream at byte %d, got %x", i, packet[i])
		}
	}
	for offset := 6; offset < len(packet); offset += 6 {
		if fmt.Sprintf("%x", packet[offset:offset+6]) != "001122334455" {
			t.Fatalf("unexpected mac repetition at %d: %x", offset, packet[offset:offset+6])
		}
	}
}

func TestWakeTargets(t *testing.T) {
	targets := wakeTargets(shared.WakeOnLANInfo{Broadcast: "10.10.10.255", Port: 7}, []string{"192.168.1.42"})
	want := []string{"10.10.10.255:7", "192.168.1.255:7", "255.255.255.255:7"}
	if fmt.Sprint(targets) != fmt.Sprint(want) {
		t.Fatalf("wakeTargets() = %+v, want %+v", targets, want)
	}
}

func TestWakeDeviceAlreadyOnline(t *testing.T) {
	result, err := WakeDevice(context.Background(), pluginhost.InventoryDevice{
		ID:         "device-1",
		Name:       "Node01",
		LastSeenAt: time.Now().UTC(),
		WakeOnLAN: shared.WakeOnLANInfo{
			Enabled:    true,
			Supported:  true,
			Active:     true,
			MACAddress: "00:11:22:33:44:55",
		},
	})
	if err != nil {
		t.Fatalf("WakeDevice: %v", err)
	}
	if result.Status != "already_online" || result.Message == "" {
		t.Fatalf("expected already_online response, got %+v", result)
	}
}

func TestWakeDeviceUnavailableEvenWhenOnline(t *testing.T) {
	_, err := WakeDevice(context.Background(), pluginhost.InventoryDevice{
		ID:         "device-1",
		Name:       "Node01",
		LastSeenAt: time.Now().UTC(),
		WakeOnLAN: shared.WakeOnLANInfo{
			Enabled: false,
			Reason:  "magic packet wake is not supported",
		},
	})
	if err == nil || !errors.Is(err, ErrWakeOnLANUnavailable) {
		t.Fatalf("expected WOL unavailable error, got %v", err)
	}
}
