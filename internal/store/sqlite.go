package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"insylus/internal/model"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS servers (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	hostname TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS principals (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	kind TEXT NOT NULL,
	notes TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS access_grants (
	id INTEGER PRIMARY KEY,
	server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	principal_id INTEGER NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
	account TEXT NOT NULL,
	sudo TEXT NOT NULL,
	notes TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(server_id, principal_id, account)
);`)
	return err
}

func (s *Store) CreateServer(ctx context.Context, in model.Server) (model.Server, error) {
	now := time.Now().UTC()
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return model.Server{}, errors.New("server name is required")
	}
	in.Hostname = strings.TrimSpace(in.Hostname)
	in.Address = strings.TrimSpace(in.Address)
	in.Notes = strings.TrimSpace(in.Notes)
	res, err := s.db.ExecContext(ctx, `INSERT INTO servers (name, hostname, address, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		in.Name, in.Hostname, in.Address, in.Notes, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return model.Server{}, err
	}
	in.ID, _ = res.LastInsertId()
	in.CreatedAt = now
	in.UpdatedAt = now
	return in, nil
}

func (s *Store) ListServers(ctx context.Context) ([]model.Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, hostname, address, notes, created_at, updated_at FROM servers ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Server{}
	for rows.Next() {
		var item model.Server
		if err := scanServer(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateServer(ctx context.Context, in model.Server) (model.Server, error) {
	now := time.Now().UTC()
	in.Name = strings.TrimSpace(in.Name)
	in.Hostname = strings.TrimSpace(in.Hostname)
	in.Address = strings.TrimSpace(in.Address)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.ID == 0 {
		return model.Server{}, errors.New("server id is required")
	}
	if in.Name == "" {
		return model.Server{}, errors.New("server name is required")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE servers SET name = ?, hostname = ?, address = ?, notes = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.Hostname, in.Address, in.Notes, now.Format(time.RFC3339), in.ID)
	if err != nil {
		return model.Server{}, err
	}
	if err := requireChanged(res, "server"); err != nil {
		return model.Server{}, err
	}
	in.UpdatedAt = now
	return in, nil
}

func (s *Store) CreatePrincipal(ctx context.Context, in model.Principal) (model.Principal, error) {
	now := time.Now().UTC()
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Name == "" {
		return model.Principal{}, errors.New("principal name is required")
	}
	if in.Kind == "" {
		in.Kind = model.PrincipalAIAgent
	}
	if !validPrincipalKind(in.Kind) {
		return model.Principal{}, fmt.Errorf("unsupported principal kind %q", in.Kind)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO principals (name, kind, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		in.Name, in.Kind, in.Notes, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return model.Principal{}, err
	}
	in.ID, _ = res.LastInsertId()
	in.CreatedAt = now
	in.UpdatedAt = now
	return in, nil
}

func (s *Store) ListPrincipals(ctx context.Context) ([]model.Principal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, notes, created_at, updated_at FROM principals ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Principal{}
	for rows.Next() {
		var item model.Principal
		if err := scanPrincipal(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePrincipal(ctx context.Context, in model.Principal) (model.Principal, error) {
	now := time.Now().UTC()
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.ID == 0 {
		return model.Principal{}, errors.New("principal id is required")
	}
	if in.Name == "" {
		return model.Principal{}, errors.New("principal name is required")
	}
	if in.Kind == "" {
		in.Kind = model.PrincipalAIAgent
	}
	if !validPrincipalKind(in.Kind) {
		return model.Principal{}, fmt.Errorf("unsupported principal kind %q", in.Kind)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE principals SET name = ?, kind = ?, notes = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.Kind, in.Notes, now.Format(time.RFC3339), in.ID)
	if err != nil {
		return model.Principal{}, err
	}
	if err := requireChanged(res, "principal"); err != nil {
		return model.Principal{}, err
	}
	in.UpdatedAt = now
	return in, nil
}

func (s *Store) CreateAccessGrant(ctx context.Context, in model.AccessGrant) (model.AccessGrant, error) {
	now := time.Now().UTC()
	if in.ServerName != "" {
		id, err := s.serverIDByName(ctx, in.ServerName)
		if err != nil {
			return model.AccessGrant{}, err
		}
		in.ServerID = id
	}
	if in.PrincipalName != "" {
		id, err := s.principalIDByName(ctx, in.PrincipalName)
		if err != nil {
			return model.AccessGrant{}, err
		}
		in.PrincipalID = id
	}
	in.Account = strings.TrimSpace(in.Account)
	in.Sudo = strings.TrimSpace(in.Sudo)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.ServerID == 0 || in.PrincipalID == 0 {
		return model.AccessGrant{}, errors.New("server and principal are required")
	}
	if in.Account == "" {
		return model.AccessGrant{}, errors.New("account is required")
	}
	if in.Sudo == "" {
		in.Sudo = model.SudoNone
	}
	if !validSudo(in.Sudo) {
		return model.AccessGrant{}, fmt.Errorf("unsupported sudo level %q", in.Sudo)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO access_grants (server_id, principal_id, account, sudo, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.ServerID, in.PrincipalID, in.Account, in.Sudo, in.Notes, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return model.AccessGrant{}, err
	}
	in.ID, _ = res.LastInsertId()
	in.CreatedAt = now
	in.UpdatedAt = now
	return in, nil
}

func (s *Store) UpdateAccessGrant(ctx context.Context, in model.AccessGrant) (model.AccessGrant, error) {
	now := time.Now().UTC()
	if in.ServerName != "" {
		id, err := s.serverIDByName(ctx, in.ServerName)
		if err != nil {
			return model.AccessGrant{}, err
		}
		in.ServerID = id
	}
	if in.PrincipalName != "" {
		id, err := s.principalIDByName(ctx, in.PrincipalName)
		if err != nil {
			return model.AccessGrant{}, err
		}
		in.PrincipalID = id
	}
	in.Account = strings.TrimSpace(in.Account)
	in.Sudo = strings.TrimSpace(in.Sudo)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.ID == 0 {
		return model.AccessGrant{}, errors.New("access grant id is required")
	}
	if in.ServerID == 0 || in.PrincipalID == 0 {
		return model.AccessGrant{}, errors.New("server and principal are required")
	}
	if in.Account == "" {
		return model.AccessGrant{}, errors.New("account is required")
	}
	if in.Sudo == "" {
		in.Sudo = model.SudoNone
	}
	if !validSudo(in.Sudo) {
		return model.AccessGrant{}, fmt.Errorf("unsupported sudo level %q", in.Sudo)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE access_grants SET server_id = ?, principal_id = ?, account = ?, sudo = ?, notes = ?, updated_at = ? WHERE id = ?`,
		in.ServerID, in.PrincipalID, in.Account, in.Sudo, in.Notes, now.Format(time.RFC3339), in.ID)
	if err != nil {
		return model.AccessGrant{}, err
	}
	if err := requireChanged(res, "access grant"); err != nil {
		return model.AccessGrant{}, err
	}
	in.UpdatedAt = now
	return in, nil
}

func (s *Store) ListAccessGrants(ctx context.Context) ([]model.AccessGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT g.id, g.server_id, s.name, g.principal_id, p.name, g.account, g.sudo, g.notes, g.created_at, g.updated_at
FROM access_grants g
JOIN servers s ON s.id = g.server_id
JOIN principals p ON p.id = g.principal_id
ORDER BY lower(s.name), lower(p.name), lower(g.account)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AccessGrant{}
	for rows.Next() {
		var item model.AccessGrant
		if err := scanAccessGrant(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) serverIDByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM servers WHERE name = ?`, strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("server %q not found", name)
	}
	return id, err
}

func (s *Store) principalIDByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM principals WHERE name = ?`, strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("principal %q not found", name)
	}
	return id, err
}

func scanServer(scanner interface{ Scan(...any) error }, item *model.Server) error {
	var created, updated string
	if err := scanner.Scan(&item.ID, &item.Name, &item.Hostname, &item.Address, &item.Notes, &created, &updated); err != nil {
		return err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return nil
}

func scanPrincipal(scanner interface{ Scan(...any) error }, item *model.Principal) error {
	var created, updated string
	if err := scanner.Scan(&item.ID, &item.Name, &item.Kind, &item.Notes, &created, &updated); err != nil {
		return err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return nil
}

func scanAccessGrant(scanner interface{ Scan(...any) error }, item *model.AccessGrant) error {
	var created, updated string
	if err := scanner.Scan(&item.ID, &item.ServerID, &item.ServerName, &item.PrincipalID, &item.PrincipalName, &item.Account, &item.Sudo, &item.Notes, &created, &updated); err != nil {
		return err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return nil
}

func validPrincipalKind(v string) bool {
	return v == model.PrincipalHuman || v == model.PrincipalService || v == model.PrincipalAIAgent
}

func validSudo(v string) bool {
	return v == model.SudoNone || v == model.SudoPrompted || v == model.SudoPasswordless
}

func requireChanged(res sql.Result, name string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s not found", name)
	}
	return nil
}
