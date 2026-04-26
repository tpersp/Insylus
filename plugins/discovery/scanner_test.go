package discovery

import (
	"context"
	"testing"
)

func TestScanSubnetReturnsCandidates(t *testing.T) {
	result, err := (lanScanner{}).ScanSubnet(context.Background(), "127.0.0.1/32")
	if err != nil {
		t.Fatalf("ScanSubnet: %v", err)
	}
	if result.Scanned != 1 {
		t.Fatalf("Scanned = %d, want 1", result.Scanned)
	}
	if result.Discovered != 1 {
		t.Fatalf("Discovered = %d, want 1", result.Discovered)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("Candidates len = %d, want 1", len(result.Candidates))
	}
	if result.Candidates[0].IPAddress != "127.0.0.1" {
		t.Fatalf("candidate IP = %q, want 127.0.0.1", result.Candidates[0].IPAddress)
	}
}

func TestSuggestedNameDoesNotInventDeviceIPName(t *testing.T) {
	if got := suggestedName("", "10.10.10.35"); got != "10.10.10.35" {
		t.Fatalf("suggestedName without hostname = %q, want IP address", got)
	}
	if got := suggestedName("Jellyfin.local", "10.10.10.35"); got != "Jellyfin" {
		t.Fatalf("suggestedName with hostname = %q, want Jellyfin", got)
	}
}
