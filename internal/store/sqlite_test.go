package store

import (
	"context"
	"path/filepath"
	"testing"

	"insylus/internal/model"
)

func TestCreateAndListAccessGrant(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "insylus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	if _, err := st.CreateServer(ctx, model.Server{Name: "atlas", Hostname: "atlas.local", Address: "10.0.0.5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePrincipal(ctx, model.Principal{Name: "codex", Kind: model.PrincipalAIAgent}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccessGrant(ctx, model.AccessGrant{
		ServerName:    "atlas",
		PrincipalName: "codex",
		Account:       "aia",
		Sudo:          model.SudoPasswordless,
		Notes:         "local automation",
	}); err != nil {
		t.Fatal(err)
	}

	grants, err := st.ListAccessGrants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	grant := grants[0]
	if grant.ServerName != "atlas" || grant.PrincipalName != "codex" || grant.Account != "aia" || grant.Sudo != model.SudoPasswordless {
		t.Fatalf("unexpected grant: %+v", grant)
	}
}

func TestUpdateAccessGrant(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "insylus.db"))
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
	if _, err := st.UpdateAccessGrant(ctx, model.AccessGrant{
		ID:            grant.ID,
		ServerName:    "atlas",
		PrincipalName: "codex",
		Account:       "ops",
		Sudo:          model.SudoPrompted,
		Notes:         "reviewed",
	}); err != nil {
		t.Fatal(err)
	}

	grants, err := st.ListAccessGrants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if grants[0].Account != "ops" || grants[0].Sudo != model.SudoPrompted || grants[0].Notes != "reviewed" {
		t.Fatalf("grant was not updated: %+v", grants[0])
	}
}

func TestDeleteAccessGrant(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "insylus.db"))
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
	if err := st.DeleteAccessGrant(ctx, grant.ID); err != nil {
		t.Fatal(err)
	}
	grants, err := st.ListAccessGrants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected deleted grant, got %+v", grants)
	}
}

func TestDeleteServerCascadesAccessGrants(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "insylus.db"))
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
	if _, err := st.CreateAccessGrant(ctx, model.AccessGrant{
		ServerID:    server.ID,
		PrincipalID: principal.ID,
		Account:     "aia",
		Sudo:        model.SudoNone,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteServer(ctx, server.ID); err != nil {
		t.Fatal(err)
	}
	grants, err := st.ListAccessGrants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected server delete to cascade grants, got %+v", grants)
	}
}
