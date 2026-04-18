package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type store struct {
	db pluginhost.DBHost
}

type scanner interface {
	Scan(dest ...any) error
}

func newStore(host pluginhost.Host) store {
	return store{db: host.DB()}
}

func (s store) listServiceInstances(ctx context.Context) ([]serviceInstanceRecord, error) {
	return s.queryServiceInstances(ctx, "", "")
}

func (s store) listServiceInstancesForDevice(ctx context.Context, deviceID string) ([]serviceInstanceRecord, error) {
	return s.queryServiceInstances(ctx, deviceID, "")
}

func (s store) searchServiceInstances(ctx context.Context, query string) ([]serviceInstanceRecord, error) {
	return s.queryServiceInstances(ctx, "", query)
}

func (s store) pruneMissingServiceInstances(ctx context.Context, deviceID, query string) (int64, error) {
	where := `where health = ?`
	args := []any{string(shared.ServiceHealthMissing)}
	if strings.TrimSpace(deviceID) != "" {
		where += ` and device_id = ?`
		args = append(args, deviceID)
	}
	if strings.TrimSpace(query) != "" {
		where += ` and (normalized_name like ? or lower(image) like ?)`
		pattern := "%" + normalizeServiceName(query) + "%"
		args = append(args, pattern, pattern)
	}
	rows, err := s.db.QueryContext(ctx, `select id, device_id, name, kind from service_instances `+where, args...)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	type pruneCandidate struct {
		id       int64
		deviceID string
		name     string
		kind     shared.WorkloadKind
	}
	var candidates []pruneCandidate
	for rows.Next() {
		var candidate pruneCandidate
		if err := rows.Scan(&candidate.id, &candidate.deviceID, &candidate.name, &candidate.kind); err != nil {
			_ = rows.Close()
			return 0, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, candidate := range candidates {
		if err := s.insertServiceEvent(ctx, candidate.id, candidate.deviceID, candidate.name, candidate.kind, "pruned", "removed missing service record", now); err != nil {
			return 0, err
		}
		if _, err := s.db.ExecContext(ctx, `delete from service_instances where id = ?`, candidate.id); err != nil {
			return 0, err
		}
	}
	return int64(len(candidates)), nil
}

func (s store) insertServiceEvent(ctx context.Context, serviceID int64, deviceID, name string, kind shared.WorkloadKind, action, details, createdAt string) error {
	var nullableID any
	if serviceID != 0 {
		nullableID = serviceID
	}
	_, err := s.db.ExecContext(ctx, `
		insert into service_events (service_instance_id, device_id, service_name, service_kind, action, details, created_at)
		values (?, ?, ?, ?, ?, ?, ?)`, nullableID, deviceID, name, string(kind), action, details, createdAt)
	return err
}

func (s store) listServiceEvents(ctx context.Context, limit int) ([]serviceEventRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		select
			e.id, e.action, e.service_name, e.service_kind, e.details, e.created_at,
			d.id, d.name, d.bootstrap_token, d.agent_token, d.hostname, d.os_name, d.ips_json, d.agent_version, d.last_seen_at, d.created_at, d.updated_at
		from service_events e
		join devices d on d.id = e.device_id
		order by e.created_at desc, e.id desc
		limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []serviceEventRecord
	for rows.Next() {
		event, err := scanServiceEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s store) queryServiceInstances(ctx context.Context, deviceID, query string) ([]serviceInstanceRecord, error) {
	sqlText := `
		select
			s.id, s.normalized_name, s.name, s.kind, s.image, s.state, s.discovered_state, s.health, s.endpoints_json,
			s.first_seen_at, s.last_seen_at, s.missing_since, s.last_reported_at, s.created_at, s.updated_at,
			d.id, d.name, d.bootstrap_token, d.agent_token, d.hostname, d.os_name, d.ips_json, d.agent_version, d.last_seen_at, d.created_at, d.updated_at
		from service_instances s
		join devices d on d.id = s.device_id`
	var args []any
	var where []string
	if strings.TrimSpace(deviceID) != "" {
		where = append(where, "s.device_id = ?")
		args = append(args, deviceID)
	}
	if strings.TrimSpace(query) != "" {
		where = append(where, "(s.normalized_name like ? or lower(s.image) like ?)")
		pattern := "%" + normalizeServiceName(query) + "%"
		args = append(args, pattern, pattern)
	}
	if len(where) > 0 {
		sqlText += "\n\t\twhere " + strings.Join(where, " and ")
	}
	sqlText += "\n\t\torder by s.normalized_name asc, d.name asc, s.kind asc"
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []serviceInstanceRecord
	for rows.Next() {
		record, err := scanServiceInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func scanServiceInstance(row scanner) (serviceInstanceRecord, error) {
	var (
		record                                                                                 serviceInstanceRecord
		endpointsJSON                                                                          string
		firstSeen, serviceLastSeen, missingSince, lastReported, serviceCreated, serviceUpdated sql.NullString
		deviceIPsJSON                                                                          string
		deviceLastSeen, deviceCreated, deviceUpdated                                           sql.NullString
	)
	if err := row.Scan(
		&record.ID, &record.NormalizedName, &record.Name, &record.Kind, &record.Image, &record.State, &record.DiscoveredState, &record.Health, &endpointsJSON,
		&firstSeen, &serviceLastSeen, &missingSince, &lastReported, &serviceCreated, &serviceUpdated,
		&record.Device.ID, &record.Device.Name, &record.Device.BootstrapToken, &record.Device.AgentToken,
		&record.Device.Hostname, &record.Device.OSName, &deviceIPsJSON, &record.Device.AgentVersion,
		&deviceLastSeen, &deviceCreated, &deviceUpdated,
	); err != nil {
		return serviceInstanceRecord{}, err
	}
	_ = json.Unmarshal([]byte(endpointsJSON), &record.Endpoints)
	_ = json.Unmarshal([]byte(deviceIPsJSON), &record.Device.IPs)
	if firstSeen.Valid {
		record.FirstSeenAt, _ = time.Parse(time.RFC3339, firstSeen.String)
	}
	if serviceLastSeen.Valid {
		record.LastSeenAt, _ = time.Parse(time.RFC3339, serviceLastSeen.String)
	}
	if missingSince.Valid {
		record.MissingSince, _ = time.Parse(time.RFC3339, missingSince.String)
	}
	if lastReported.Valid {
		record.LastReportedAt, _ = time.Parse(time.RFC3339, lastReported.String)
	}
	if serviceCreated.Valid {
		record.CreatedAt, _ = time.Parse(time.RFC3339, serviceCreated.String)
	}
	if serviceUpdated.Valid {
		record.UpdatedAt, _ = time.Parse(time.RFC3339, serviceUpdated.String)
	}
	if deviceLastSeen.Valid {
		record.Device.LastSeenAt, _ = time.Parse(time.RFC3339, deviceLastSeen.String)
	}
	if deviceCreated.Valid {
		record.Device.CreatedAt, _ = time.Parse(time.RFC3339, deviceCreated.String)
	}
	if deviceUpdated.Valid {
		record.Device.UpdatedAt, _ = time.Parse(time.RFC3339, deviceUpdated.String)
	}
	if record.Health == "" {
		record.Health = shared.ServiceHealthUnknown
	}
	return record, nil
}

func scanServiceEvent(row scanner) (serviceEventRecord, error) {
	var (
		event                                        serviceEventRecord
		created                                      string
		deviceIPsJSON                                string
		deviceLastSeen, deviceCreated, deviceUpdated sql.NullString
	)
	if err := row.Scan(
		&event.ID, &event.Action, &event.ServiceName, &event.ServiceKind, &event.Details, &created,
		&event.Device.ID, &event.Device.Name, &event.Device.BootstrapToken, &event.Device.AgentToken,
		&event.Device.Hostname, &event.Device.OSName, &deviceIPsJSON, &event.Device.AgentVersion,
		&deviceLastSeen, &deviceCreated, &deviceUpdated,
	); err != nil {
		return serviceEventRecord{}, err
	}
	event.CreatedAt, _ = time.Parse(time.RFC3339, created)
	_ = json.Unmarshal([]byte(deviceIPsJSON), &event.Device.IPs)
	if deviceLastSeen.Valid {
		event.Device.LastSeenAt, _ = time.Parse(time.RFC3339, deviceLastSeen.String)
	}
	if deviceCreated.Valid {
		event.Device.CreatedAt, _ = time.Parse(time.RFC3339, deviceCreated.String)
	}
	if deviceUpdated.Valid {
		event.Device.UpdatedAt, _ = time.Parse(time.RFC3339, deviceUpdated.String)
	}
	return event, nil
}
