package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"insylus/internal/pluginhost"
)

type targetService struct {
	store *Store
}

func (s *Store) targetService() targetService {
	return targetService{store: s}
}

func (s targetService) List(ctx context.Context) ([]pluginhost.Target, error) {
	rows, err := s.store.db.QueryContext(ctx, `
		select id, name, kind, hostname, ips_json, tags_json, note, created_by, created_at, updated_at
		from targets
		order by name collate nocase asc`)
	if err != nil {
		return nil, err
	}
	out := []pluginhost.Target{}
	for rows.Next() {
		target, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.attachTargetDetails(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s targetService) Get(ctx context.Context, id string) (pluginhost.Target, error) {
	row := s.store.db.QueryRowContext(ctx, `
		select id, name, kind, hostname, ips_json, tags_json, note, created_by, created_at, updated_at
		from targets
		where id = ?`, strings.TrimSpace(id))
	target, err := scanTarget(row)
	if err != nil {
		return pluginhost.Target{}, err
	}
	if err := s.attachTargetDetails(ctx, &target); err != nil {
		return pluginhost.Target{}, err
	}
	return target, nil
}

func (s targetService) Find(ctx context.Context, query string) ([]pluginhost.Target, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	targets, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []pluginhost.Target
	for _, target := range targets {
		if strings.EqualFold(target.ID, query) ||
			strings.EqualFold(target.Name, query) ||
			strings.EqualFold(target.Hostname, query) ||
			strings.EqualFold(target.APIURL, query) ||
			strings.EqualFold(target.SSHHost, query) ||
			hasString(target.IPs, query) {
			out = append(out, target)
		}
	}
	if len(out) == 0 {
		return nil, sql.ErrNoRows
	}
	return out, nil
}

func (s targetService) Create(ctx context.Context, input pluginhost.TargetInput) (pluginhost.Target, error) {
	input = normalizeTargetInput(input)
	if input.Name == "" {
		return pluginhost.Target{}, fmt.Errorf("name is required")
	}
	if input.Kind == "" {
		input.Kind = "target"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := randomToken(12)
	bootstrapToken := randomToken(24)
	ipsJSON, _ := json.Marshal(input.IPs)
	tagsJSON, _ := json.Marshal(input.Tags)
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return pluginhost.Target{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into devices (id, name, bootstrap_token, created_at, updated_at)
		values (?, ?, ?, ?, ?)`,
		id, input.Name, bootstrapToken, now, now); err != nil {
		_ = tx.Rollback()
		return pluginhost.Target{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into targets (id, name, kind, hostname, ips_json, tags_json, note, created_by, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.Name, input.Kind, input.Hostname, string(ipsJSON), string(tagsJSON), input.Note, input.CreatedBy, now, now); err != nil {
		_ = tx.Rollback()
		return pluginhost.Target{}, err
	}
	if err := upsertTargetAddressesTx(ctx, tx, id, input, now); err != nil {
		_ = tx.Rollback()
		return pluginhost.Target{}, err
	}
	if input.CreatedBy != "" && len(input.Metadata) > 0 {
		metadataJSON, _ := json.Marshal(input.Metadata)
		if _, err := tx.ExecContext(ctx, `
			insert into target_metadata (target_id, plugin_id, metadata_json, updated_at)
			values (?, ?, ?, ?)`,
			id, input.CreatedBy, string(metadataJSON), now); err != nil {
			_ = tx.Rollback()
			return pluginhost.Target{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return pluginhost.Target{}, err
	}
	return s.Get(ctx, id)
}

func (s targetService) Update(ctx context.Context, id string, input pluginhost.TargetInput) (pluginhost.Target, error) {
	input = normalizeTargetInput(input)
	current, err := s.Get(ctx, id)
	if err != nil {
		return pluginhost.Target{}, err
	}
	if input.Name == "" {
		input.Name = current.Name
	}
	if input.Kind == "" {
		input.Kind = current.Kind
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ipsJSON, _ := json.Marshal(input.IPs)
	tagsJSON, _ := json.Marshal(input.Tags)
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return pluginhost.Target{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		update devices
		set name = ?, updated_at = ?
		where id = ?`,
		input.Name, now, id); err != nil {
		_ = tx.Rollback()
		if isUniqueConstraint(err, "devices_name_unique") {
			return pluginhost.Target{}, ErrDuplicateDeviceName
		}
		return pluginhost.Target{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		update targets
		set name = ?, kind = ?, hostname = ?, ips_json = ?, tags_json = ?, note = ?, updated_at = ?
		where id = ?`,
		input.Name, input.Kind, input.Hostname, string(ipsJSON), string(tagsJSON), input.Note, now, id); err != nil {
		_ = tx.Rollback()
		return pluginhost.Target{}, err
	}
	if err := upsertTargetAddressesTx(ctx, tx, id, input, now); err != nil {
		_ = tx.Rollback()
		return pluginhost.Target{}, err
	}
	if err := tx.Commit(); err != nil {
		return pluginhost.Target{}, err
	}
	return s.Get(ctx, id)
}

func (s targetService) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `delete from targets where id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `delete from devices where id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s targetService) SetMetadata(ctx context.Context, targetID, pluginID string, metadata map[string]any) error {
	targetID = strings.TrimSpace(targetID)
	pluginID = strings.TrimSpace(pluginID)
	if targetID == "" || pluginID == "" {
		return fmt.Errorf("target_id and plugin_id are required")
	}
	raw, _ := json.Marshal(metadata)
	_, err := s.store.db.ExecContext(ctx, `
		insert into target_metadata (target_id, plugin_id, metadata_json, updated_at)
		values (?, ?, ?, datetime('now'))
		on conflict(target_id, plugin_id) do update set
			metadata_json = excluded.metadata_json,
			updated_at = excluded.updated_at`,
		targetID, pluginID, string(raw))
	return err
}

func (s targetService) Metadata(ctx context.Context, targetID string) (map[string]map[string]any, error) {
	rows, err := s.store.db.QueryContext(ctx, `
		select plugin_id, metadata_json
		from target_metadata
		where target_id = ?`, strings.TrimSpace(targetID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var pluginID, raw string
		if err := rows.Scan(&pluginID, &raw); err != nil {
			return nil, err
		}
		var metadata map[string]any
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &metadata)
		}
		if metadata == nil {
			metadata = map[string]any{}
		}
		out[pluginID] = metadata
	}
	return out, rows.Err()
}

func (s targetService) PurgePlugin(ctx context.Context, pluginID string) error {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return fmt.Errorf("plugin_id is required")
	}
	_, err := s.store.db.ExecContext(ctx, `delete from target_metadata where plugin_id = ?`, pluginID)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTarget(scanner rowScanner) (pluginhost.Target, error) {
	var target pluginhost.Target
	var ipsJSON, tagsJSON, createdAt, updatedAt string
	if err := scanner.Scan(&target.ID, &target.Name, &target.Kind, &target.Hostname, &ipsJSON, &tagsJSON, &target.Note, &target.CreatedBy, &createdAt, &updatedAt); err != nil {
		return pluginhost.Target{}, err
	}
	_ = json.Unmarshal([]byte(ipsJSON), &target.IPs)
	_ = json.Unmarshal([]byte(tagsJSON), &target.Tags)
	target.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	target.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if target.Kind == "" {
		target.Kind = "target"
	}
	return target, nil
}

func (s targetService) attachTargetDetails(ctx context.Context, target *pluginhost.Target) error {
	rows, err := s.store.db.QueryContext(ctx, `select kind, value from target_addresses where target_id = ?`, target.ID)
	if err != nil {
		return err
	}
	addresses := map[string]string{}
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			_ = rows.Close()
			return err
		}
		addresses[kind] = value
	}
	if err := rows.Close(); err != nil {
		return err
	}
	target.APIURL = addresses["api_url"]
	target.SSHHost = addresses["ssh_host"]
	target.SSHUser = addresses["ssh_user"]
	metadata, err := s.Metadata(ctx, target.ID)
	if err != nil {
		return err
	}
	target.Metadata = metadata
	return nil
}

func normalizeTargetInput(input pluginhost.TargetInput) pluginhost.TargetInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Hostname = strings.TrimSpace(input.Hostname)
	input.APIURL = strings.TrimRight(strings.TrimSpace(input.APIURL), "/")
	input.SSHHost = strings.TrimSpace(input.SSHHost)
	input.SSHUser = strings.TrimSpace(input.SSHUser)
	input.Note = strings.TrimSpace(input.Note)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	input.IPs = cleanStrings(input.IPs)
	input.Tags = cleanStrings(input.Tags)
	if input.Addresses != nil {
		input.APIURL = firstNonEmpty(input.APIURL, input.Addresses["api_url"])
		input.SSHHost = firstNonEmpty(input.SSHHost, input.Addresses["ssh_host"])
		input.SSHUser = firstNonEmpty(input.SSHUser, input.Addresses["ssh_user"])
	}
	return input
}

func upsertTargetAddressesTx(ctx context.Context, tx *sql.Tx, targetID string, input pluginhost.TargetInput, now string) error {
	values := map[string]string{
		"api_url":  input.APIURL,
		"ssh_host": input.SSHHost,
		"ssh_user": input.SSHUser,
	}
	for kind, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			if _, err := tx.ExecContext(ctx, `delete from target_addresses where target_id = ? and kind = ?`, targetID, kind); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			insert into target_addresses (target_id, kind, value, created_at, updated_at)
			values (?, ?, ?, ?, ?)
			on conflict(target_id, kind) do update set
				value = excluded.value,
				updated_at = excluded.updated_at`,
			targetID, kind, value, now, now); err != nil {
			return err
		}
	}
	return nil
}

func cleanStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func hasString(values []string, query string) bool {
	for _, value := range values {
		if strings.EqualFold(value, query) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sqlNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
