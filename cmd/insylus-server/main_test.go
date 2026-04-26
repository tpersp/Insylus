package main

import (
	"net/http"
	"reflect"
	"testing"
)

func TestSplitManagedGroups(t *testing.T) {
	got := splitManagedGroups("adm, systemd-journal,,wheel ")
	want := []string{"adm", "systemd-journal", "wheel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitManagedGroups() = %+v, want %+v", got, want)
	}
}

func TestSplitManagedGroupsEmpty(t *testing.T) {
	if got := splitManagedGroups("  "); got != nil {
		t.Fatalf("splitManagedGroups() = %+v, want nil", got)
	}
}

func TestNewHTTPServerUsesDefensiveTimeouts(t *testing.T) {
	handler := http.NewServeMux()
	srv := newHTTPServer(":0", handler)

	if srv.Addr != ":0" {
		t.Fatalf("Addr = %q, want :0", srv.Addr)
	}
	if srv.Handler != handler {
		t.Fatal("Handler was not preserved")
	}
	if srv.ReadHeaderTimeout != serverReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", srv.ReadHeaderTimeout, serverReadHeaderTimeout)
	}
	if srv.IdleTimeout != serverIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", srv.IdleTimeout, serverIdleTimeout)
	}
}
