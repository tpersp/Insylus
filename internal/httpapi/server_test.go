package httpapi

import (
	"testing"

	"insylus/internal/model"
)

func TestBuildDashboardRowsIncludesAccessAndServersWithoutAccess(t *testing.T) {
	rows := buildDashboardRows(
		[]model.Server{
			{Name: "atlas", Hostname: "atlas.local", Address: "10.0.0.5", Notes: "controller"},
			{Name: "pi", Address: "10.0.0.9", Notes: "unassigned"},
		},
		[]model.Principal{{Name: "codex", Kind: model.PrincipalAIAgent, Notes: "default coding agent"}},
		[]model.AccessGrant{{
			ServerName:    "atlas",
			PrincipalName: "codex",
			Account:       "aia",
			Sudo:          model.SudoPasswordless,
			Notes:         "not shown on dashboard",
		}},
	)

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if !rows[0].HasAccess || rows[0].PrincipalKind != model.PrincipalAIAgent {
		t.Fatalf("expected access row with principal kind, got %+v", rows[0])
	}
	if rows[0].ServerNotes != "controller" || rows[0].PrincipalNotes != "default coding agent" {
		t.Fatalf("expected server and principal notes on access row, got %+v", rows[0])
	}
	if rows[1].ServerName != "pi" || rows[1].HasAccess {
		t.Fatalf("expected server without access row, got %+v", rows[1])
	}
	if rows[1].ServerNotes != "unassigned" {
		t.Fatalf("expected server notes on unassigned row, got %+v", rows[1])
	}
}
