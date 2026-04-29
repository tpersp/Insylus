package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveServedAgentBinaryPathFallsBackFromArmToArmv7(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "insylus-agent")
	armv7Path := filepath.Join(dir, "insylus-agent-linux-armv7")
	if err := os.WriteFile(defaultPath, []byte("default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(armv7Path, []byte("armv7"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveServedAgentBinaryPath(defaultPath, "linux", "arm")
	if err != nil {
		t.Fatal(err)
	}
	if got != armv7Path {
		t.Fatalf("resolveServedAgentBinaryPath() = %q, want %q", got, armv7Path)
	}
}

func TestResolveServedAgentBinaryPathUsesSpecificArch(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "insylus-agent")
	arm64Path := filepath.Join(dir, "insylus-agent-linux-arm64")
	if err := os.WriteFile(defaultPath, []byte("default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arm64Path, []byte("arm64"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveServedAgentBinaryPath(defaultPath, "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if got != arm64Path {
		t.Fatalf("resolveServedAgentBinaryPath() = %q, want %q", got, arm64Path)
	}
}
