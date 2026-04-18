package jellyfin

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

// setToken stores or updates a Jellyfin API token for a device.
func (s store) setToken(ctx context.Context, item jellyfinToken) (jellyfinTokenSummary, error) {
	item.DeviceID = strings.TrimSpace(item.DeviceID)
	item.ServerName = strings.TrimSpace(item.ServerName)
	item.APIURL = strings.TrimRight(strings.TrimSpace(item.APIURL), "/")
	item.APIKey = strings.TrimSpace(item.APIKey)
	item.DefaultUserID = strings.TrimSpace(item.DefaultUserID)
	item.DefaultUsername = strings.TrimSpace(item.DefaultUsername)
	if item.DeviceID == "" {
		return jellyfinTokenSummary{}, fmt.Errorf("device_id is required")
	}
	if item.ServerName == "" {
		return jellyfinTokenSummary{}, fmt.Errorf("server_name is required")
	}
	var encryptedKey string
	if item.APIKey != "" {
		var err error
		encryptedKey, err = s.secrets.Encrypt(item.APIKey)
		if err != nil {
			return jellyfinTokenSummary{}, err
		}
	} else {
		existing, err := s.getToken(ctx, item.DeviceID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return jellyfinTokenSummary{}, fmt.Errorf("api_key is required")
			}
			return jellyfinTokenSummary{}, err
		}
		encryptedKey, err = s.secrets.Encrypt(existing.APIKey)
		if err != nil {
			return jellyfinTokenSummary{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		insert into jellyfin_tokens (
			device_id, server_name, api_url, api_key_encrypted, default_user_id, default_username, tls_insecure, created_at, updated_at
		)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(device_id) do update set
			server_name = excluded.server_name,
			api_url = excluded.api_url,
			api_key_encrypted = excluded.api_key_encrypted,
			default_user_id = excluded.default_user_id,
			default_username = excluded.default_username,
			tls_insecure = excluded.tls_insecure,
			updated_at = excluded.updated_at`,
		item.DeviceID, item.ServerName, item.APIURL, encryptedKey, item.DefaultUserID, item.DefaultUsername, boolInt(item.TLSInsecure), now, now)
	if err != nil {
		return jellyfinTokenSummary{}, err
	}
	return s.getTokenSummary(ctx, item.DeviceID)
}

// getToken retrieves the full token record for a device.
func (s store) getToken(ctx context.Context, deviceID string) (jellyfinToken, error) {
	row := s.db.QueryRowContext(ctx, `
		select device_id, server_name, api_url, api_key_encrypted, default_user_id, default_username, tls_insecure, created_at, updated_at
		from jellyfin_tokens
		where device_id = ?`, strings.TrimSpace(deviceID))
	item, err := s.scanToken(row)
	if err != nil {
		return jellyfinToken{}, err
	}
	device, err := s.inventory.GetDevice(ctx, item.DeviceID)
	if err == nil {
		enrichTokenWithDevice(&item, device)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return jellyfinToken{}, err
	}
	return item, nil
}

// getTokenSummary returns a lightweight summary of the token configuration.
func (s store) getTokenSummary(ctx context.Context, deviceID string) (jellyfinTokenSummary, error) {
	item, err := s.getToken(ctx, deviceID)
	if err != nil {
		return jellyfinTokenSummary{}, err
	}
	return summarizeToken(item), nil
}

// deleteToken removes the token for a device.
func (s store) deleteToken(ctx context.Context, deviceID string) error {
	res, err := s.db.ExecContext(ctx, `delete from jellyfin_tokens where device_id = ?`, strings.TrimSpace(deviceID))
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

// listConfiguredServers returns all devices with Jellyfin tokens.
func (s store) listConfiguredServers(ctx context.Context) ([]jellyfinTokenSummary, error) {
	devices, err := s.inventory.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	tokens, err := s.tokenSummaries(ctx)
	if err != nil {
		return nil, err
	}
	var out []jellyfinTokenSummary
	for _, device := range devices {
		item, hasToken := tokens[device.ID]
		isJellyfin := device.DeviceType == "jellyfin-server" ||
			device.Purpose == "jellyfin-server" ||
			device.DiscoveredType == "jellyfin-server" ||
			device.DiscoveredRole == "jellyfin-server"
		if !isJellyfin && !hasToken {
			continue
		}
		if !hasToken {
			item = jellyfinTokenSummary{
				DeviceID:   device.ID,
				DeviceName: device.Name,
				Hostname:   device.Hostname,
				ServerName: defaultServerName(device),
				HasToken:   false,
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// resolveDevice finds a device by various identifiers.
func (s store) resolveDevice(ctx context.Context, query string) (pluginhost.InventoryDevice, jellyfinTokenSummary, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, fmt.Errorf("device or server name is required")
	}
	devices, err := s.inventory.ListDevices(ctx)
	if err != nil {
		return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, err
	}
	tokens, err := s.tokenSummaries(ctx)
	if err != nil {
		return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, err
	}
	var matches []pluginhost.InventoryDevice
	for _, device := range devices {
		item := tokens[device.ID]
		if strings.EqualFold(device.ID, query) ||
			strings.EqualFold(device.Name, query) ||
			strings.EqualFold(device.Hostname, query) ||
			hasIP(device.IPs, query) ||
			strings.EqualFold(item.ServerName, query) {
			matches = append(matches, device)
		}
	}
	if len(matches) == 0 {
		return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, sql.ErrNoRows
	}
	if len(matches) > 1 {
		return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, fmt.Errorf("multiple devices match %q", query)
	}
	return matches[0], tokens[matches[0].ID], nil
}

func (s store) tokens(ctx context.Context) (map[string]jellyfinToken, error) {
	rows, err := s.db.QueryContext(ctx, `
		select device_id, server_name, api_url, api_key_encrypted, default_user_id, default_username, tls_insecure, created_at, updated_at
		from jellyfin_tokens
		order by server_name asc`)
	if err != nil {
		return nil, err
	}
	out := map[string]jellyfinToken{}
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

func (s store) tokenSummaries(ctx context.Context) (map[string]jellyfinTokenSummary, error) {
	tokens, err := s.tokens(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]jellyfinTokenSummary{}
	for deviceID, item := range tokens {
		out[deviceID] = summarizeToken(item)
	}
	return out, nil
}

type tokenScanner interface {
	Scan(dest ...any) error
}

func (s store) scanToken(scanner tokenScanner) (jellyfinToken, error) {
	var (
		item          jellyfinToken
		encryptedKey  string
		tlsInsecure   int
		createdAtText string
		updatedAtText string
	)
	if err := scanner.Scan(
		&item.DeviceID, &item.ServerName, &item.APIURL, &encryptedKey, &item.DefaultUserID, &item.DefaultUsername, &tlsInsecure,
		&createdAtText, &updatedAtText,
	); err != nil {
		return jellyfinToken{}, err
	}
	key, err := s.secrets.Decrypt(encryptedKey)
	if err != nil {
		return jellyfinToken{}, err
	}
	item.APIKey = key
	item.TLSInsecure = tlsInsecure == 1
	if item.CreatedAt, err = time.Parse(time.RFC3339, createdAtText); err != nil {
		return jellyfinToken{}, err
	}
	if item.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtText); err != nil {
		return jellyfinToken{}, err
	}
	return item, nil
}

func summarizeToken(item jellyfinToken) jellyfinTokenSummary {
	return jellyfinTokenSummary{
		DeviceID:        item.DeviceID,
		DeviceName:      item.DeviceName,
		Hostname:        item.DeviceHost,
		ServerName:      item.ServerName,
		APIURL:          item.APIURL,
		DefaultUserID:   item.DefaultUserID,
		DefaultUsername: item.DefaultUsername,
		HasToken:        item.APIKey != "",
		TLSInsecure:     item.TLSInsecure,
		UpdatedAt:       item.UpdatedAt,
	}
}

func enrichTokenWithDevice(item *jellyfinToken, device pluginhost.InventoryDevice) {
	item.DeviceName = device.Name
	item.DeviceHost = device.Hostname
	item.DeviceOnline = deviceOnline(device.LastSeenAt)
}

func defaultServerName(device pluginhost.InventoryDevice) string {
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
