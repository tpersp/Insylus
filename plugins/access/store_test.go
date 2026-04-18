package access

import "testing"

func TestFingerprintAuthorizedKey(t *testing.T) {
	fingerprint, err := fingerprintAuthorizedKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAxWnKk4PSJVd0v8wM4OMwph0l7Qtf8f4lA5lUwWfKcG operator@atlas")
	if err != nil {
		t.Fatalf("fingerprintAuthorizedKey: %v", err)
	}
	if fingerprint != "SHA256:jeabRz42uGiOUjjXfifzGYYjWx5cOViLncROVLNkbr0" {
		t.Fatalf("fingerprint = %q", fingerprint)
	}
}

func TestFingerprintAuthorizedKeyRejectsInvalidInput(t *testing.T) {
	if _, err := fingerprintAuthorizedKey("not a public key"); err == nil {
		t.Fatal("expected invalid key error")
	}
}
