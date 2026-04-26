package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunMainPrintsUsageForMissingCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := runMain([]string{"insylus-agent"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage: insylus-agent") {
		t.Fatalf("stderr did not contain usage: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunMainReportsConfigLoadFailureWithoutPanic(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := runMain([]string{"insylus-agent", "run", "--config", "/no/such/insylus-agent.json"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "run failed:") {
		t.Fatalf("stderr did not contain clean failure: %q", stderr.String())
	}
}

func TestRunMainVersionWritesStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := runMain([]string{"insylus-agent", "version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("version output was empty")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
