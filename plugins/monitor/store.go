package monitor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"insylus/internal/pluginhost"
)

const (
	defaultIntervalSeconds = 300
	defaultTimeoutMillis   = 2000
)

type store struct {
	db pluginhost.DBHost
}

func newStore(host pluginhost.Host) store {
	return store{db: host.DB()}
}

func (s store) Settings(ctx context.Context) (Settings, error) {
	settings := Settings{
		IntervalSeconds: defaultIntervalSeconds,
		TimeoutMillis:   defaultTimeoutMillis,
	}
	rows, err := s.db.QueryContext(ctx, `select key, value from monitor_settings`)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		switch key {
		case "interval_seconds":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
				settings.IntervalSeconds = n
			}
		case "timeout_millis":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
				settings.TimeoutMillis = n
			}
		}
	}
	return settings, rows.Err()
}

func (s store) SaveSettings(ctx context.Context, settings Settings) error {
	now := time.Now().UTC().Format(time.RFC3339)
	pairs := map[string]string{
		"interval_seconds": strconv.Itoa(settings.IntervalSeconds),
		"timeout_millis":   strconv.Itoa(settings.TimeoutMillis),
	}
	for key, value := range pairs {
		if _, err := s.db.ExecContext(ctx, `
			insert into monitor_settings (key, value, updated_at)
			values (?, ?, ?)
			on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`,
			key, value, now); err != nil {
			return err
		}
	}
	return nil
}

func (s store) ListManualTargets(ctx context.Context) ([]ManualTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, name, host, port, enabled, created_at, updated_at
		from monitor_manual_targets
		order by lower(name), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []ManualTarget
	for rows.Next() {
		var target ManualTarget
		var enabled int64
		var createdAt, updatedAt string
		if err := rows.Scan(&target.ID, &target.Name, &target.Host, &target.Port, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		target.Enabled = enabled == 1
		target.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		target.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (s store) AddManualTarget(ctx context.Context, target ManualTarget) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		insert into monitor_manual_targets (name, host, port, enabled, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(target.Name),
		strings.TrimSpace(target.Host),
		target.Port,
		boolInt(target.Enabled),
		now,
		now,
	)
	return err
}

func (s store) DeleteManualTarget(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `delete from monitor_manual_targets where id = ?`, id)
	return err
}

func (s store) RecordSamples(ctx context.Context, samples []sampleRecord) error {
	now := time.Now().UTC()
	for _, sample := range samples {
		checkedAt := sample.CheckedAt
		if checkedAt.IsZero() {
			checkedAt = now
		}
		if _, err := s.db.ExecContext(ctx, `
			insert into monitor_samples (
				target_key, target_name, source, device_id, host, port, success, latency_ms, error, checked_at
			) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sample.Key,
			sample.Name,
			sample.Source,
			sample.DeviceID,
			sample.Host,
			sample.Port,
			boolInt(sample.Success),
			sample.LatencyMs,
			sample.Error,
			checkedAt.UTC().Format(time.RFC3339),
		); err != nil {
			return err
		}
	}
	cutoff := now.Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `delete from monitor_samples where checked_at < ?`, cutoff)
	return err
}

func (s store) LatestStatuses(ctx context.Context, targets []monitorTarget) ([]Status, error) {
	summaries, err := s.summaryMap(ctx)
	if err != nil {
		return nil, err
	}
	statuses := make([]Status, 0, len(targets))
	for _, target := range targets {
		status := Status{
			Key:            target.Key,
			Source:         target.Source,
			DeviceID:       target.DeviceID,
			ManualTargetID: target.ManualTargetID,
			Name:           target.Name,
			Host:           target.Host,
			Port:           target.Port,
			Enabled:        target.Enabled,
			State:          "unknown",
			MonitorMethod:  target.MonitorMethod,
		}
		if !target.Enabled {
			status.State = "paused"
			status.LastError = "manual target disabled"
		}
		if target.Host == "" {
			if status.LastError == "" {
				status.LastError = "no address configured"
			}
			statuses = append(statuses, status)
			continue
		}
		summary, ok := summaries[target.Key]
		if !ok {
			if status.LastError == "" {
				status.LastError = "no checks yet"
			}
			statuses = append(statuses, status)
			continue
		}
		status.LastCheckedAt = summary.lastCheckedAt
		status.LatencyMs = summary.lastLatencyMs
		status.Availability24h = summary.availability24h
		status.LastError = summary.lastError
		status.Samples24h = summary.samples24h
		status.Successes24h = summary.successes24h
		status.HasRecentData = true
		if !target.Enabled {
			status.State = "paused"
		} else if summary.lastSuccess {
			status.State = "up"
		} else {
			status.State = "down"
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (s store) History(ctx context.Context, key string, window time.Duration) ([]HistoryPoint, error) {
	cutoff := time.Now().UTC().Add(-window).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
		select checked_at, success, latency_ms, error
		from monitor_samples
		where target_key = ? and checked_at >= ?
		order by checked_at asc`, key, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []HistoryPoint
	for rows.Next() {
		var point HistoryPoint
		var checkedAt string
		var success int64
		if err := rows.Scan(&checkedAt, &success, &point.LatencyMs, &point.Error); err != nil {
			return nil, err
		}
		point.CheckedAt, _ = time.Parse(time.RFC3339, checkedAt)
		point.Success = success == 1
		points = append(points, point)
	}
	return points, rows.Err()
}

type summary struct {
	lastCheckedAt   time.Time
	lastSuccess     bool
	lastLatencyMs   float64
	lastError       string
	availability24h float64
	samples24h      int
	successes24h    int
}

func (s store) summaryMap(ctx context.Context) (map[string]summary, error) {
	rows, err := s.db.QueryContext(ctx, `
		select target_key, checked_at, success, latency_ms, error
		from monitor_samples
		order by checked_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	out := map[string]summary{}
	for rows.Next() {
		var key, checkedAt, errText string
		var success int64
		var latency float64
		if err := rows.Scan(&key, &checkedAt, &success, &latency, &errText); err != nil {
			return nil, err
		}
		parsedAt, _ := time.Parse(time.RFC3339, checkedAt)
		item := out[key]
		if item.lastCheckedAt.IsZero() {
			item.lastCheckedAt = parsedAt
			item.lastSuccess = success == 1
			item.lastLatencyMs = latency
			item.lastError = errText
		}
		if !parsedAt.Before(cutoff) {
			item.samples24h++
			if success == 1 {
				item.successes24h++
			}
		}
		out[key] = item
	}
	for key, item := range out {
		if item.samples24h > 0 {
			item.availability24h = float64(item.successes24h) * 100 / float64(item.samples24h)
		}
		out[key] = item
	}
	return out, rows.Err()
}

func normalizeSettings(intervalSeconds, timeoutMillis int) Settings {
	if intervalSeconds < 30 {
		intervalSeconds = defaultIntervalSeconds
	}
	if timeoutMillis < 250 {
		timeoutMillis = defaultTimeoutMillis
	}
	return Settings{
		IntervalSeconds: intervalSeconds,
		TimeoutMillis:   timeoutMillis,
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func monitorKeyForManual(id int64) string {
	return fmt.Sprintf("manual-%d", id)
}
