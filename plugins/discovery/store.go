package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"insylus/internal/pluginhost"
)

type store struct {
	db      pluginhost.DBHost
	targets pluginhost.TargetService
}

func newStore(host pluginhost.Host) store {
	return store{db: host.DB(), targets: host.Targets()}
}

func (s store) listCandidates(ctx context.Context) ([]candidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, display_name, hostname, ip_address, mac_address, open_ports_json, status, status_note,
			source_cidr, kind_hint, coalesce(promoted_target_id, ''), first_seen_at, last_seen_at, updated_at
		from discovered_devices
		order by
			case status
				when 'pending' then 0
				when 'ignored' then 1
				when 'promoted' then 2
				else 3
			end,
			ip_address asc,
			last_seen_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []candidate
	for rows.Next() {
		item, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachKnownTargets(ctx, out); err != nil {
		return nil, err
	}
	sortCandidates(out)
	return out, nil
}

func (s store) saveScan(ctx context.Context, subnet string, scan scanResponse) ([]candidate, error) {
	now := time.Now().UTC()
	txResult := make([]candidate, 0, len(scan.Candidates))
	for _, found := range scan.Candidates {
		item, err := s.upsertCandidate(ctx, subnet, scanResult{
			DisplayName: found.DisplayName,
			Hostname:    found.Hostname,
			IPAddress:   found.IPAddress,
			MACAddress:  found.MACAddress,
			OpenPorts:   found.OpenPorts,
			KindHint:    found.KindHint,
		}, now)
		if err != nil {
			return nil, err
		}
		txResult = append(txResult, item)
	}
	return txResult, nil
}

func (s store) upsertCandidate(ctx context.Context, subnet string, result scanResult, now time.Time) (candidate, error) {
	fingerprint := fingerprintForResult(result)
	openPortsJSON, _ := json.Marshal(result.OpenPorts)
	updated := now.Format(time.RFC3339)
	firstSeen := updated

	var id int64
	var existingStatus, existingPromoted string
	var existingFirstSeen string
	err := s.db.QueryRowContext(ctx, `
		select id, status, coalesce(promoted_target_id, ''), first_seen_at
		from discovered_devices
		where fingerprint = ?`, fingerprint).Scan(&id, &existingStatus, &existingPromoted, &existingFirstSeen)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, execErr := s.db.ExecContext(ctx, `
			insert into discovered_devices (
				fingerprint, display_name, hostname, ip_address, mac_address, open_ports_json,
				status, status_note, source_cidr, kind_hint, first_seen_at, last_seen_at, created_at, updated_at
			)
			values (?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?)`,
			fingerprint, result.DisplayName, result.Hostname, result.IPAddress, result.MACAddress, string(openPortsJSON),
			statusPending, subnet, result.KindHint, firstSeen, updated, updated, updated)
		if execErr != nil {
			return candidate{}, execErr
		}
		id, _ = res.LastInsertId()
	case err != nil:
		return candidate{}, err
	default:
		_, err = s.db.ExecContext(ctx, `
			update discovered_devices
			set display_name = ?, hostname = ?, ip_address = ?, mac_address = ?, open_ports_json = ?,
				source_cidr = ?, kind_hint = ?, last_seen_at = ?, updated_at = ?
			where id = ?`,
			result.DisplayName, result.Hostname, result.IPAddress, result.MACAddress, string(openPortsJSON),
			subnet, result.KindHint, updated, updated, id)
		if err != nil {
			return candidate{}, err
		}
		firstSeen = existingFirstSeen
		_ = existingStatus
		_ = existingPromoted
	}
	return s.getCandidate(ctx, id)
}

func (s store) getCandidate(ctx context.Context, id int64) (candidate, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, display_name, hostname, ip_address, mac_address, open_ports_json, status, status_note,
			source_cidr, kind_hint, coalesce(promoted_target_id, ''), first_seen_at, last_seen_at, updated_at
		from discovered_devices
		where id = ?`, id)
	return scanCandidate(row)
}

func (s store) setStatus(ctx context.Context, id int64, status string) (candidate, error) {
	status = strings.TrimSpace(status)
	switch status {
	case statusPending, statusIgnored:
	default:
		return candidate{}, fmt.Errorf("invalid status %q", status)
	}
	item, err := s.getCandidate(ctx, id)
	if err != nil {
		return candidate{}, err
	}
	items := []candidate{item}
	if err := s.attachKnownTargets(ctx, items); err != nil {
		return candidate{}, err
	}
	item = items[0]
	if item.KnownTargetID != "" && item.PromotedTargetID == "" {
		return candidate{}, fmt.Errorf("known inventory device cannot change discovery status")
	}
	_, err = s.db.ExecContext(ctx, `
		update discovered_devices
		set status = ?, promoted_target_id = case when ? = 'pending' then null else promoted_target_id end,
			status_note = '', updated_at = ?
		where id = ?`, status, status, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return candidate{}, err
	}
	item, err = s.getCandidate(ctx, id)
	if err != nil {
		return candidate{}, err
	}
	items = []candidate{item}
	if err := s.attachKnownTargets(ctx, items); err != nil {
		return candidate{}, err
	}
	return items[0], nil
}

func (s store) promoteCandidate(ctx context.Context, id int64) (candidate, pluginhost.Target, error) {
	item, err := s.getCandidate(ctx, id)
	if err != nil {
		return candidate{}, pluginhost.Target{}, err
	}
	items := []candidate{item}
	if err := s.attachKnownTargets(ctx, items); err != nil {
		return candidate{}, pluginhost.Target{}, err
	}
	item = items[0]
	if item.PromotedTargetID != "" {
		target, getErr := s.targets.Get(ctx, item.PromotedTargetID)
		if getErr == nil {
			return item, target, nil
		}
	}
	if item.KnownTargetID != "" {
		target, getErr := s.targets.Get(ctx, item.KnownTargetID)
		if getErr == nil {
			return item, target, fmt.Errorf("device already exists in inventory as %q", target.Name)
		}
		return item, pluginhost.Target{}, fmt.Errorf("device already exists in inventory")
	}

	name, err := s.uniqueTargetName(ctx, item.DisplayName)
	if err != nil {
		return candidate{}, pluginhost.Target{}, err
	}
	note := buildPromotionNote(item)
	target, err := s.targets.Create(ctx, pluginhost.TargetInput{
		Name:      name,
		Kind:      firstNonEmpty(item.KindHint, "linux-host"),
		Hostname:  item.Hostname,
		IPs:       []string{item.IPAddress},
		SSHHost:   item.IPAddress,
		CreatedBy: "discovery",
		Note:      note,
		Metadata: map[string]any{
			"source_cidr": item.SourceCIDR,
			"mac_address": item.MACAddress,
			"open_ports":  item.OpenPorts,
		},
	})
	if err != nil {
		return candidate{}, pluginhost.Target{}, err
	}

	_, err = s.db.ExecContext(ctx, `
		update discovered_devices
		set status = ?, status_note = ?, promoted_target_id = ?, updated_at = ?
		where id = ?`,
		statusPromoted, "added to inventory as inventory-only", target.ID, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return candidate{}, pluginhost.Target{}, err
	}
	item, err = s.getCandidate(ctx, id)
	if err != nil {
		return candidate{}, pluginhost.Target{}, err
	}
	items = []candidate{item}
	if err := s.attachKnownTargets(ctx, items); err != nil {
		return candidate{}, pluginhost.Target{}, err
	}
	return items[0], target, nil
}

func (s store) uniqueTargetName(ctx context.Context, raw string) (string, error) {
	base := sanitizeName(raw)
	if base == "" {
		base = "device"
	}
	name := base
	for suffix := 1; suffix < 1000; suffix++ {
		var count int
		if err := s.db.QueryRowContext(ctx, `select count(*) from devices where lower(name) = lower(?)`, name).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return name, nil
		}
		name = base + "-" + strconv.Itoa(suffix+1)
	}
	return "", fmt.Errorf("could not allocate unique device name for %q", base)
}

func scanCandidate(scanner interface{ Scan(dest ...any) error }) (candidate, error) {
	var item candidate
	var portsJSON string
	var firstSeen, lastSeen, updated string
	if err := scanner.Scan(
		&item.ID,
		&item.DisplayName,
		&item.Hostname,
		&item.IPAddress,
		&item.MACAddress,
		&portsJSON,
		&item.Status,
		&item.StatusNote,
		&item.SourceCIDR,
		&item.KindHint,
		&item.PromotedTargetID,
		&firstSeen,
		&lastSeen,
		&updated,
	); err != nil {
		return candidate{}, err
	}
	_ = json.Unmarshal([]byte(portsJSON), &item.OpenPorts)
	sort.Ints(item.OpenPorts)
	item.FirstSeenAt, _ = time.Parse(time.RFC3339, firstSeen)
	item.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeen)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return item, nil
}

func fingerprintForResult(result scanResult) string {
	if mac := strings.ToLower(strings.TrimSpace(result.MACAddress)); mac != "" {
		return "mac:" + mac
	}
	if host := strings.ToLower(strings.TrimSpace(result.Hostname)); host != "" {
		return "host:" + host
	}
	return "ip:" + strings.TrimSpace(result.IPAddress)
}

func buildPromotionNote(item candidate) string {
	parts := []string{"Discovered by Insylus network discovery"}
	if item.SourceCIDR != "" {
		parts = append(parts, "source subnet "+item.SourceCIDR)
	}
	if item.MACAddress != "" {
		parts = append(parts, "MAC "+item.MACAddress)
	}
	if len(item.OpenPorts) > 0 {
		ports := make([]string, 0, len(item.OpenPorts))
		for _, port := range item.OpenPorts {
			ports = append(ports, strconv.Itoa(port))
		}
		parts = append(parts, "open TCP ports "+strings.Join(ports, ", "))
	}
	return strings.Join(parts, " | ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ipStringList(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip == nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s store) attachKnownTargets(ctx context.Context, items []candidate) error {
	if len(items) == 0 {
		return nil
	}
	targets, err := s.targets.List(ctx)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].PromotedTargetID != "" {
			continue
		}
		target, ok := matchKnownTarget(items[i], targets)
		if !ok {
			continue
		}
		items[i].KnownTargetID = target.ID
		items[i].KnownTargetName = target.Name
		items[i].Status = "known"
		if items[i].StatusNote == "" {
			items[i].StatusNote = "already present in devices"
		}
	}
	return nil
}

func matchKnownTarget(item candidate, targets []pluginhost.Target) (pluginhost.Target, bool) {
	itemIP := strings.TrimSpace(item.IPAddress)
	itemHost := strings.TrimSpace(item.Hostname)
	itemName := strings.TrimSpace(item.DisplayName)
	for _, target := range targets {
		for _, ip := range target.IPs {
			if strings.TrimSpace(ip) == itemIP && itemIP != "" {
				return target, true
			}
		}
		if itemHost != "" && strings.EqualFold(strings.TrimSpace(target.Hostname), itemHost) {
			return target, true
		}
		if itemName != "" && strings.EqualFold(strings.TrimSpace(target.Name), itemName) {
			return target, true
		}
	}
	return pluginhost.Target{}, false
}

func sortCandidates(items []candidate) {
	sort.SliceStable(items, func(i, j int) bool {
		if cmp := compareIPs(items[i].IPAddress, items[j].IPAddress); cmp != 0 {
			return cmp < 0
		}
		leftRank := candidateStatusRank(items[i])
		rightRank := candidateStatusRank(items[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return strings.ToLower(items[i].DisplayName) < strings.ToLower(items[j].DisplayName)
	})
}

func candidateStatusRank(item candidate) int {
	switch item.Status {
	case "known":
		return 0
	case statusPending:
		return 1
	case statusIgnored:
		return 2
	case statusPromoted:
		return 3
	default:
		return 4
	}
}
