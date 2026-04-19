package update

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"insylus/internal/pluginhost"
)

type store struct {
	db pluginhost.DBHost
}

func newStore(host pluginhost.Host) store {
	return store{
		db: host.DB(),
	}
}

// GetUpdateSettings returns update-related app settings.
func (s store) GetUpdateSettings(ctx context.Context) (autoUpdateEnabled bool, updateChannel string, lastCheckedAt *time.Time, skippedVersion string, err error) {
	autoUpdateEnabled, err = s.getBoolSetting(ctx, "server_auto_update_enabled")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}

	updateChannel, err = s.getStringSetting(ctx, "server_update_channel")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}

	lastCheckedAtStr, err := s.getStringSetting(ctx, "last_checked_at")
	if err == nil && lastCheckedAtStr != "" {
		t, parseErr := time.Parse(time.RFC3339, lastCheckedAtStr)
		if parseErr == nil {
			lastCheckedAt = &t
		}
	}

	skippedVersion, err = s.getStringSetting(ctx, "skipped_version")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}

	return autoUpdateEnabled, updateChannel, lastCheckedAt, skippedVersion, nil
}

// SetAutoUpdateEnabled sets the auto update enabled setting.
func (s store) SetAutoUpdateEnabled(ctx context.Context, enabled bool) error {
	return s.setBoolSetting(ctx, "server_auto_update_enabled", enabled)
}

// SetUpdateChannel sets the update channel.
func (s store) SetUpdateChannel(ctx context.Context, channel string) error {
	return s.setStringSetting(ctx, "server_update_channel", channel)
}

// SetLastCheckedAt sets the last checked timestamp.
func (s store) SetLastCheckedAt(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return s.setStringSetting(ctx, "last_checked_at", now)
}

// SetSkippedVersion sets the skipped version.
func (s store) SetSkippedVersion(ctx context.Context, version string) error {
	return s.setStringSetting(ctx, "skipped_version", version)
}

// ClearSkippedVersion clears the skipped version.
func (s store) ClearSkippedVersion(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "delete from app_settings where key = 'skipped_version'")
	return err
}

// ListUpdates returns all update history records.
func (s store) ListUpdates(ctx context.Context) ([]UpdateStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, version, released_at, status, notes, applied_at, created_at
		from server_updates
		order by created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var updates []UpdateStatus
	for rows.Next() {
		var u UpdateStatus
		var notes sql.NullString
		var appliedAt sql.NullString
		if err := rows.Scan(&u.ID, &u.Version, &u.ReleasedAt, &u.Status, &notes, &appliedAt, &u.CreatedAt); err != nil {
			return nil, err
		}
		if notes.Valid {
			u.Notes = notes.String
		}
		if appliedAt.Valid {
			u.AppliedAt = appliedAt.String
		}
		updates = append(updates, u)
	}
	return updates, rows.Err()
}

// GetLatestUpdate returns the most recent update record.
func (s store) GetLatestUpdate(ctx context.Context) (UpdateStatus, error) {
	var u UpdateStatus
	var notes sql.NullString
	var appliedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		select id, version, released_at, status, notes, applied_at, created_at
		from server_updates
		order by created_at desc
		limit 1
	`).Scan(&u.ID, &u.Version, &u.ReleasedAt, &u.Status, &notes, &appliedAt, &u.CreatedAt)
	if err != nil {
		return UpdateStatus{}, err
	}
	if notes.Valid {
		u.Notes = notes.String
	}
	if appliedAt.Valid {
		u.AppliedAt = appliedAt.String
	}
	return u, nil
}

// CreateUpdate creates a new update record.
func (s store) CreateUpdate(ctx context.Context, version, releasedAt, status, notes string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		insert into server_updates (version, released_at, status, notes, created_at)
		values (?, ?, ?, ?, datetime('now'))
	`, version, releasedAt, status, notes)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateUpdateStatus updates the status of an update record.
func (s store) UpdateUpdateStatus(ctx context.Context, id int64, status string) error {
	var query string
	var args []any
	if status == "applied" {
		query = "update server_updates set status = ?, applied_at = datetime('now') where id = ?"
	} else {
		query = "update server_updates set status = ? where id = ?"
	}
	args = append(args, status, id)
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// getBoolSetting returns a boolean setting.
func (s store) getBoolSetting(ctx context.Context, key string) (bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "select value from app_settings where key = ?", key).Scan(&value)
	if err != nil {
		return false, err
	}
	return value == "true" || value == "1", nil
}

// getStringSetting returns a string setting.
func (s store) getStringSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "select value from app_settings where key = ?", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// setBoolSetting sets a boolean setting.
func (s store) setBoolSetting(ctx context.Context, key string, value bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	boolStr := "false"
	if value {
		boolStr = "true"
	}
	_, err := s.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values (?, ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at
	`, key, boolStr, now)
	return err
}

// setStringSetting sets a string setting.
func (s store) setStringSetting(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values (?, ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at
	`, key, value, now)
	return err
}
