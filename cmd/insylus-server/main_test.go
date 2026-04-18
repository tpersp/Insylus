package main

import (
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
