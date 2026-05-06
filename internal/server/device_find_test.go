package server

import (
	"testing"

	"insylus/internal/shared"
)

func TestFindMatchingDevicesFindsPartialName(t *testing.T) {
	records := []DeviceRecord{
		{Device: shared.Device{ID: "dev-1", Name: "Zeta-pve", Hostname: "zeta-pve"}},
		{Device: shared.Device{ID: "dev-2", Name: "PiZ2Monitor1", Hostname: "piz2monitor1"}},
	}

	matches := findMatchingDevices(records, "pve")
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1: %+v", len(matches), matches)
	}
	if matches[0].Device.ID != "dev-1" {
		t.Fatalf("match ID = %q, want dev-1", matches[0].Device.ID)
	}
}

func TestFindMatchingDevicesPrefersExactMatch(t *testing.T) {
	records := []DeviceRecord{
		{Device: shared.Device{ID: "dev-1", Name: "zeta", Hostname: "zeta"}},
		{Device: shared.Device{ID: "dev-2", Name: "Zeta-pve", Hostname: "zeta-pve"}},
	}

	matches := findMatchingDevices(records, "zeta")
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1: %+v", len(matches), matches)
	}
	if matches[0].Device.ID != "dev-1" {
		t.Fatalf("match ID = %q, want dev-1", matches[0].Device.ID)
	}
}

func TestFindMatchingDevicesReturnsMultiplePartialMatches(t *testing.T) {
	records := []DeviceRecord{
		{Device: shared.Device{ID: "dev-1", Name: "Zeta-pve", Hostname: "zeta-pve"}},
		{Device: shared.Device{ID: "dev-2", Name: "Beta-pve", Hostname: "beta-pve"}},
	}

	matches := findMatchingDevices(records, "pve")
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2: %+v", len(matches), matches)
	}
}
