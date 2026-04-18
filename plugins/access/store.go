package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"

	"golang.org/x/crypto/ssh"
)

type store struct {
	db pluginhost.DBHost
}

func newStore(host pluginhost.Host) store {
	return store{db: host.DB()}
}

func (s store) createSSHKey(ctx context.Context, name, publicKey string) (shared.SSHKey, error) {
	fingerprint, err := fingerprintAuthorizedKey(publicKey)
	if err != nil {
		return shared.SSHKey{}, err
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		insert into ssh_keys (name, public_key, fingerprint, created_at)
		values (?, ?, ?, ?)`, name, publicKey, fingerprint, now.Format(time.RFC3339))
	if err != nil {
		return shared.SSHKey{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return shared.SSHKey{}, err
	}
	return shared.SSHKey{ID: id, Name: name, PublicKey: publicKey, Fingerprint: fingerprint, CreatedAt: now}, nil
}

func (s store) listSSHKeys(ctx context.Context) ([]shared.SSHKey, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, public_key, fingerprint, created_at from ssh_keys order by name asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []shared.SSHKey
	for rows.Next() {
		var key shared.SSHKey
		var createdAt string
		if err := rows.Scan(&key.ID, &key.Name, &key.PublicKey, &key.Fingerprint, &createdAt); err != nil {
			return nil, err
		}
		key.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s store) updatePolicy(ctx context.Context, deviceID string, enabled bool, mode shared.AccessMode, sshKeyID *int64) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		update device_access_policies
		set managed_account_enabled = ?, access_mode = ?, ssh_key_id = ?, policy_revision = policy_revision + 1, updated_at = ?
		where device_id = ?`, boolInt(enabled), string(mode), sshKeyID, now.Format(time.RFC3339), deviceID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s store) setDeviceMode(ctx context.Context, deviceID string, mode shared.DeviceMode) error {
	if mode != shared.DeviceModeInventoryOnly && mode != shared.DeviceModeAccessManaged {
		return fmt.Errorf("invalid device mode: %s", mode)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var (
		res sql.Result
		err error
	)
	if mode == shared.DeviceModeAccessManaged {
		res, err = s.db.ExecContext(ctx, `
			update device_access_policies
			set device_mode = ?, managed_account_enabled = 0, access_mode = 'disabled', ssh_key_id = null, policy_revision = policy_revision + 1, updated_at = ?
			where device_id = ? and coalesce(device_mode, 'inventory-only') <> ?`,
			string(mode), now, deviceID, string(mode))
	} else {
		res, err = s.db.ExecContext(ctx, `
			update device_access_policies
			set device_mode = ?, policy_revision = policy_revision + 1, updated_at = ?
			where device_id = ? and coalesce(device_mode, 'inventory-only') <> ?`,
			string(mode), now, deviceID, string(mode))
	}
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `select count(*) from device_access_policies where device_id = ?`, deviceID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

func (s store) agentAutoUpdateDefault(ctx context.Context) (bool, error) {
	row := s.db.QueryRowContext(ctx, `select value from app_settings where key = 'agent_auto_update_default'`)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return strings.EqualFold(value, "true") || value == "1", nil
}

func (s store) managedAccountConfig(ctx context.Context) (shared.ManagedAccountConfig, error) {
	user, err := s.appSetting(ctx, "managed_user")
	if err != nil {
		return shared.ManagedAccountConfig{}, err
	}
	groupsRaw, err := s.appSetting(ctx, "managed_groups")
	if err != nil {
		return shared.ManagedAccountConfig{}, err
	}
	cfg := shared.ManagedAccountConfig{
		ManagedUser:   strings.TrimSpace(user),
		ManagedGroups: splitManagedGroups(groupsRaw),
	}
	if cfg.ManagedUser == "" {
		cfg.ManagedUser = shared.DefaultManagedUser
	}
	if len(cfg.ManagedGroups) == 0 {
		cfg.ManagedGroups = []string{"adm", "systemd-journal"}
	}
	return cfg, nil
}

func (s store) setManagedAccountConfig(ctx context.Context, cfg shared.ManagedAccountConfig) error {
	cfg.ManagedUser = strings.TrimSpace(cfg.ManagedUser)
	if cfg.ManagedUser == "" {
		return fmt.Errorf("managed user is required")
	}
	cfg.ManagedGroups = splitManagedGroups(strings.Join(cfg.ManagedGroups, ","))
	if len(cfg.ManagedGroups) == 0 {
		return fmt.Errorf("at least one managed group is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('managed_user', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`, cfg.ManagedUser, now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('managed_groups', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`, strings.Join(cfg.ManagedGroups, ","), now)
	return err
}

func (s store) appSetting(ctx context.Context, key string) (string, error) {
	row := s.db.QueryRowContext(ctx, `select value from app_settings where key = ?`, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func (s store) setAgentAutoUpdateDefault(ctx context.Context, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('agent_auto_update_default', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`, boolString(enabled), now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		update device_agent_updates
		set effective_enabled = ?, updated_at = ?
		where auto_update_override = 'inherit'`, boolInt(enabled), now)
	return err
}

func (s store) setAgentAutoUpdateOverride(ctx context.Context, deviceID string, override shared.AgentAutoUpdateOverride) error {
	if override == "" {
		override = shared.AgentAutoUpdateInherit
	}
	if override != shared.AgentAutoUpdateInherit && override != shared.AgentAutoUpdateEnabled && override != shared.AgentAutoUpdateDisabled {
		return fmt.Errorf("invalid auto-update override: %s", override)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		update device_agent_updates
		set auto_update_override = ?, updated_at = ?
		where device_id = ?`, string(override), now, deviceID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	global, err := s.agentAutoUpdateDefault(ctx)
	if err != nil {
		return err
	}
	effective := global
	switch override {
	case shared.AgentAutoUpdateEnabled:
		effective = true
	case shared.AgentAutoUpdateDisabled:
		effective = false
	}
	_, err = s.db.ExecContext(ctx, `
		update device_agent_updates
		set effective_enabled = ?, updated_at = ?
		where device_id = ?`, boolInt(effective), now, deviceID)
	return err
}

func fingerprintAuthorizedKey(publicKey string) (string, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(key), nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func splitManagedGroups(raw string) []string {
	parts := strings.Split(raw, ",")
	groups := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		group := strings.TrimSpace(part)
		if group == "" {
			continue
		}
		key := strings.ToLower(group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		groups = append(groups, group)
	}
	return groups
}
