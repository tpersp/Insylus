package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"insylus/internal/model"
	"insylus/internal/store"
)

func TestBuildDashboardRowsIncludesAccessAndServersWithoutAccess(t *testing.T) {
	rows := buildDashboardRows(
		[]model.Server{
			{Name: "atlas", Hostname: "atlas.local", Address: "10.0.0.5", Notes: "controller"},
			{Name: "pi", Address: "10.0.0.9", Notes: "unassigned"},
		},
		[]model.Principal{{Name: "codex", Kind: model.PrincipalAIAgent, Notes: "default coding agent"}},
		[]model.AccessGrant{
			{
				ServerName:    "atlas",
				PrincipalName: "codex",
				Account:       "aia",
				Sudo:          model.SudoPasswordless,
				Notes:         "automation",
			},
			{
				ServerName:    "atlas",
				PrincipalName: "doden",
				Account:       "aia",
				Sudo:          model.SudoPrompted,
			},
		},
	)

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ServerName != "atlas" || !rows[0].HasAccess || len(rows[0].Grants) != 2 {
		t.Fatalf("expected grouped access row for atlas, got %+v", rows[0])
	}
	if rows[0].Grants[0].PrincipalKind != model.PrincipalAIAgent || rows[0].Grants[0].Notes != "automation" {
		t.Fatalf("expected grant details on grouped row, got %+v", rows[0].Grants[0])
	}
	if rows[0].ServerNotes != "controller" {
		t.Fatalf("expected server notes on grouped row, got %+v", rows[0])
	}
	if rows[1].ServerName != "pi" || rows[1].HasAccess {
		t.Fatalf("expected server without access row, got %+v", rows[1])
	}
	if rows[1].ServerNotes != "unassigned" {
		t.Fatalf("expected server notes on unassigned row, got %+v", rows[1])
	}
}

func TestDeleteAccessGrantEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "insylus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	server, err := st.CreateServer(ctx, model.Server{Name: "atlas"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := st.CreatePrincipal(ctx, model.Principal{Name: "codex", Kind: model.PrincipalAIAgent})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := st.CreateAccessGrant(ctx, model.AccessGrant{
		ServerID:    server.ID,
		PrincipalID: principal.ID,
		Account:     "aia",
		Sudo:        model.SudoNone,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/access?id="+strconv.FormatInt(grant.ID, 10), nil)
	rec := httptest.NewRecorder()
	New(st).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from delete, got %d: %s", rec.Code, rec.Body.String())
	}
	grants, err := st.ListAccessGrants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected deleted grant, got %+v", grants)
	}
}
