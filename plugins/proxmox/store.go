package proxmox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"insylus/internal/pluginhost"
)

type store struct {
	db        pluginhost.DBHost
	secrets   pluginhost.SecretHost
	inventory pluginhost.InventoryService
	targets   pluginhost.TargetService
}

func newStore(host pluginhost.Host) store {
	return store{
		db:        host.DB(),
		secrets:   host.Secrets(),
		inventory: host.Data().Inventory(),
		targets:   host.Targets(),
	}
}

func (s store) setToken(ctx context.Context, item token) (tokenSummary, error) {
	item.DeviceID = strings.TrimSpace(item.DeviceID)
	item.NodeName = strings.TrimSpace(item.NodeName)
	item.APIURL = strings.TrimRight(strings.TrimSpace(item.APIURL), "/")
	item.TokenID = strings.TrimSpace(item.TokenID)
	item.TokenSecret = strings.TrimSpace(item.TokenSecret)
	item.Role = strings.TrimSpace(item.Role)
	if item.DeviceID == "" {
		return tokenSummary{}, fmt.Errorf("device_id is required")
	}
	if item.NodeName == "" {
		return tokenSummary{}, fmt.Errorf("node_name is required")
	}
	if item.TokenID == "" {
		return tokenSummary{}, fmt.Errorf("token_id is required")
	}
	var encryptedSecret string
	if item.TokenSecret != "" {
		var err error
		encryptedSecret, err = s.secrets.Encrypt(item.TokenSecret)
		if err != nil {
			return tokenSummary{}, err
		}
	} else {
		existing, err := s.getToken(ctx, item.DeviceID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return tokenSummary{}, fmt.Errorf("token_secret is required")
			}
			return tokenSummary{}, err
		}
		encryptedSecret, err = s.secrets.Encrypt(existing.TokenSecret)
		if err != nil {
			return tokenSummary{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		insert into proxmox_tokens (
			device_id, node_name, api_url, token_id, token_secret_encrypted, role, tls_insecure, created_at, updated_at
		)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(device_id) do update set
			node_name = excluded.node_name,
			api_url = excluded.api_url,
			token_id = excluded.token_id,
			token_secret_encrypted = excluded.token_secret_encrypted,
			role = excluded.role,
			tls_insecure = excluded.tls_insecure,
			updated_at = excluded.updated_at`,
		item.DeviceID, item.NodeName, item.APIURL, item.TokenID, encryptedSecret, item.Role, boolInt(item.TLSInsecure), now, now)
	if err != nil {
		return tokenSummary{}, err
	}
	return s.getTokenSummary(ctx, item.DeviceID)
}

func (s store) getToken(ctx context.Context, deviceID string) (token, error) {
	row := s.db.QueryRowContext(ctx, `
		select device_id, node_name, api_url, token_id, token_secret_encrypted, role, tls_insecure, created_at, updated_at
		from proxmox_tokens
		where device_id = ?`, strings.TrimSpace(deviceID))
	item, err := s.scanToken(row)
	if err != nil {
		return token{}, err
	}
	device, err := s.inventory.GetDevice(ctx, item.DeviceID)
	if err == nil {
		enrichTokenWithDevice(&item, device)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return token{}, err
	}
	return item, nil
}

func (s store) getTokenSummary(ctx context.Context, deviceID string) (tokenSummary, error) {
	item, err := s.getToken(ctx, deviceID)
	if err != nil {
		return tokenSummary{}, err
	}
	return summarizeToken(item), nil
}

func (s store) deleteToken(ctx context.Context, deviceID string) error {
	res, err := s.db.ExecContext(ctx, `delete from proxmox_tokens where device_id = ?`, strings.TrimSpace(deviceID))
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

func (s store) listNodes(ctx context.Context) ([]tokenSummary, error) {
	devices, err := s.inventory.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	tokenByDevice, err := s.tokenSummaries(ctx)
	if err != nil {
		return nil, err
	}
	var out []tokenSummary
	for _, device := range devices {
		item, hasToken := tokenByDevice[device.ID]
		isProxmox := device.DeviceType == "proxmox-node" ||
			device.Purpose == "proxmox-node" ||
			device.DiscoveredType == "proxmox-node" ||
			device.DiscoveredRole == "proxmox-node"
		if !isProxmox && !hasToken {
			continue
		}
		if !hasToken {
			item = tokenSummary{
				DeviceID:   device.ID,
				DeviceName: device.Name,
				Hostname:   device.Hostname,
				NodeName:   defaultNodeName(device),
				HasToken:   false,
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s store) resolveDeviceForConfig(ctx context.Context, query string) (pluginhost.InventoryDevice, tokenSummary, error) {
	device, summary, err := s.resolveDevice(ctx, query)
	if err != nil {
		return pluginhost.InventoryDevice{}, tokenSummary{}, err
	}
	if summary.DeviceID == "" {
		summary = tokenSummary{
			DeviceID:   device.ID,
			DeviceName: device.Name,
			Hostname:   device.Hostname,
			NodeName:   defaultNodeName(device),
		}
	}
	return device, summary, nil
}

func (s store) resolveDevice(ctx context.Context, query string) (pluginhost.InventoryDevice, tokenSummary, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return pluginhost.InventoryDevice{}, tokenSummary{}, fmt.Errorf("node is required")
	}
	devices, err := s.inventory.ListDevices(ctx)
	if err != nil {
		return pluginhost.InventoryDevice{}, tokenSummary{}, err
	}
	tokens, err := s.tokenSummaries(ctx)
	if err != nil {
		return pluginhost.InventoryDevice{}, tokenSummary{}, err
	}
	var matches []pluginhost.InventoryDevice
	for _, device := range devices {
		item := tokens[device.ID]
		if strings.EqualFold(device.ID, query) ||
			strings.EqualFold(device.Name, query) ||
			strings.EqualFold(device.Hostname, query) ||
			hasIP(device.IPs, query) ||
			strings.EqualFold(item.NodeName, query) {
			matches = append(matches, device)
		}
	}
	if len(matches) == 0 {
		return pluginhost.InventoryDevice{}, tokenSummary{}, sql.ErrNoRows
	}
	if len(matches) > 1 {
		return pluginhost.InventoryDevice{}, tokenSummary{}, fmt.Errorf("multiple devices match %q", query)
	}
	return matches[0], tokens[matches[0].ID], nil
}

func (s store) tokens(ctx context.Context) (map[string]token, error) {
	rows, err := s.db.QueryContext(ctx, `
		select device_id, node_name, api_url, token_id, token_secret_encrypted, role, tls_insecure, created_at, updated_at
		from proxmox_tokens
		order by node_name asc`)
	if err != nil {
		return nil, err
	}
	out := map[string]token{}
	for rows.Next() {
		item, err := s.scanToken(rows)
		if err != nil {
			return nil, err
		}
		out[item.DeviceID] = item
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	devices, err := s.inventory.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	for _, device := range devices {
		item, ok := out[device.ID]
		if !ok {
			continue
		}
		enrichTokenWithDevice(&item, device)
		out[device.ID] = item
	}
	return out, nil
}

func (s store) tokenSummaries(ctx context.Context) (map[string]tokenSummary, error) {
	tokens, err := s.tokens(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]tokenSummary{}
	for deviceID, item := range tokens {
		out[deviceID] = summarizeToken(item)
	}
	return out, nil
}

type tokenScanner interface {
	Scan(dest ...any) error
}

func (s store) scanToken(scanner tokenScanner) (token, error) {
	var (
		item            token
		encryptedSecret string
		tlsInsecure     int
		createdAtText   string
		updatedAtText   string
	)
	if err := scanner.Scan(
		&item.DeviceID, &item.NodeName, &item.APIURL, &item.TokenID, &encryptedSecret, &item.Role, &tlsInsecure,
		&createdAtText, &updatedAtText,
	); err != nil {
		return token{}, err
	}
	secret, err := s.secrets.Decrypt(encryptedSecret)
	if err != nil {
		return token{}, err
	}
	item.TokenSecret = secret
	item.TLSInsecure = tlsInsecure == 1
	if item.CreatedAt, err = time.Parse(time.RFC3339, createdAtText); err != nil {
		return token{}, err
	}
	if item.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtText); err != nil {
		return token{}, err
	}
	return item, nil
}

func summarizeToken(item token) tokenSummary {
	return tokenSummary{
		DeviceID:    item.DeviceID,
		DeviceName:  item.DeviceName,
		Hostname:    item.DeviceHost,
		NodeName:    item.NodeName,
		APIURL:      item.APIURL,
		TokenID:     item.TokenID,
		Role:        item.Role,
		HasToken:    item.TokenID != "",
		TLSInsecure: item.TLSInsecure,
		UpdatedAt:   item.UpdatedAt,
	}
}

func enrichTokenWithDevice(item *token, device pluginhost.InventoryDevice) {
	item.DeviceName = device.Name
	item.DeviceHost = device.Hostname
	item.DeviceOnline = deviceOnline(device.LastSeenAt)
}

func defaultNodeName(device pluginhost.InventoryDevice) string {
	if strings.TrimSpace(device.Hostname) != "" {
		return device.Hostname
	}
	return device.Name
}

func hasIP(ips []string, query string) bool {
	for _, ip := range ips {
		if ip == query {
			return true
		}
	}
	return false
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func deviceOnline(lastSeen time.Time) bool {
	return !lastSeen.IsZero() && time.Since(lastSeen) <= 45*time.Second
}
