package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"insylus/internal/shared"
)

func installAgentUpdate(ctx context.Context, manifest shared.AgentUpdateManifest) error {
	installedAgentPath := installPathsFromEnv().BinaryPath
	dir := filepath.Dir(installedAgentPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".insylus-agent.update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.DownloadURL, nil)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_ = tmp.Close()
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	gotSHA := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(gotSHA, manifest.SHA256) {
		_ = tmp.Close()
		return fmt.Errorf("checksum mismatch: got %s want %s", gotSHA, manifest.SHA256)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := validateAgentBinary(tmpPath, manifest.ServerAgentVersion); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, installedAgentPath); err != nil {
		return err
	}
	return nil
}

func validateAgentBinary(path, expectedVersion string) error {
	cmd := exec.Command(path, "version")
	cmd.Env = append(os.Environ(), "INSYLUS_AGENT_VALIDATE=1")
	cmd.Stdout = nil
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("validate updated agent: %w", err)
	}
	got := strings.TrimSpace(string(out))
	if got != expectedVersion {
		return fmt.Errorf("validate updated agent version: got %q want %q", got, expectedVersion)
	}
	return nil
}

func compareVersions(a, b string) int {
	ap := parseVersionParts(a)
	bp := parseVersionParts(b)
	maxLen := len(ap)
	if len(bp) > maxLen {
		maxLen = len(bp)
	}
	for i := 0; i < maxLen; i++ {
		var av, bv int
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parseVersionParts(v string) []int {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" {
		return nil
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func sleepBeforeExit() {
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
}
