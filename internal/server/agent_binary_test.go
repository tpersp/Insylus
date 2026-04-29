package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAgentBinaryPathFallsBackFromArmToArmv7(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "insylus-agent")
	armv7Path := filepath.Join(dir, "insylus-agent-linux-armv7")
	if err := os.WriteFile(defaultPath, []byte("default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(armv7Path, []byte("armv7"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveAgentBinaryPath(defaultPath, "linux", "arm")
	if err != nil {
		t.Fatal(err)
	}
	if got != armv7Path {
		t.Fatalf("resolveAgentBinaryPath() = %q, want %q", got, armv7Path)
	}
}

func TestResolveAgentBinaryPathReturnsDefaultWithoutPlatform(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "insylus-agent")

	got, err := resolveAgentBinaryPath(defaultPath, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != defaultPath {
		t.Fatalf("resolveAgentBinaryPath() = %q, want %q", got, defaultPath)
	}
}
