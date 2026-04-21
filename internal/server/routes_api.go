package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"insylus/internal/shared"
	"insylus/internal/version"
)

type inventoryView string

const (
	inventoryViewCompact inventoryView = "compact"
	inventoryViewInfo    inventoryView = "info"
	inventoryViewFull    inventoryView = "full"
)

func (a *App) handleBootstrapRegister(w http.ResponseWriter, r *http.Request) {
	var req shared.BootstrapRequest
	if !a.decodeJSON(w, r, &req) {
		return
	}
	resp, err := a.store.RegisterAgent(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	var req shared.CheckInRequest
	if !a.decodeJSON(w, r, &req) {
		return
	}
	if err := a.store.UpdateCheckIn(r.Context(), device.ID, req.Health, req.AgentInstall); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handlePolicyFetch(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	policy, err := a.store.GetPolicyForDevice(r.Context(), device.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	policy, err = a.withManagedAccountPolicy(r.Context(), policy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	manifest, err := a.agentUpdateManifest(r, device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	policy.AgentUpdate = manifest
	a.writeJSON(w, http.StatusOK, policy)
}

func (a *App) ManagedAccountConfig(ctx context.Context) (shared.ManagedAccountConfig, error) {
	return a.store.ManagedAccountConfig(ctx, shared.ManagedAccountConfig{
		ManagedUser:   a.configuredManagedUser(),
		ManagedGroups: a.configuredManagedGroups(),
	})
}

func (a *App) configuredManagedUser() string {
	user := strings.TrimSpace(a.cfg.ManagedUser)
	if user == "" {
		return shared.DefaultManagedUser
	}
	return user
}

func (a *App) configuredManagedGroups() []string {
	groups := normalizedManagedGroups(a.cfg.ManagedGroups)
	if len(groups) == 0 {
		return defaultManagedGroups()
	}
	return groups
}

func normalizedManagedGroups(groups []string) []string {
	out := make([]string, 0, len(groups))
	seen := map[string]struct{}{}
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		key := strings.ToLower(group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, group)
	}
	return out
}

func defaultManagedGroups() []string {
	return []string{"adm", "systemd-journal"}
}

func managedGroupsForAccessMode(mode shared.AccessMode, auditGroups []string) []string {
	base := auditBaseGroups(auditGroups)
	if len(base) == 0 {
		base = defaultManagedGroups()
	}
	switch mode {
	case shared.AccessModeDocker:
		return appendUniqueGroup(base, "docker")
	case shared.AccessModeAudit, shared.AccessModeSudoPrompted, shared.AccessModeSudoPasswordless:
		return base
	default:
		return nil
	}
}

func auditBaseGroups(groups []string) []string {
	out := normalizedManagedGroups(groups)
	filtered := out[:0]
	for _, group := range out {
		switch strings.ToLower(strings.TrimSpace(group)) {
		case "docker":
			continue
		default:
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func appendUniqueGroup(groups []string, group string) []string {
	out := append([]string(nil), groups...)
	key := strings.ToLower(strings.TrimSpace(group))
	if key == "" {
		return out
	}
	for _, existing := range out {
		if strings.ToLower(strings.TrimSpace(existing)) == key {
			return out
		}
	}
	return append(out, strings.TrimSpace(group))
}

func (a *App) handleAgentUpdateStatus(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	var report shared.AgentUpdateReport
	if !a.decodeJSON(w, r, &report) {
		return
	}
	if err := a.store.SaveAgentUpdateStatus(r.Context(), device.ID, report); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (a *App) handleReport(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	var report shared.DeviceReport
	if !a.decodeJSON(w, r, &report) {
		return
	}
	if err := a.store.SaveReport(r.Context(), token, report); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (a *App) handleDevicesInventory(w http.ResponseWriter, r *http.Request) {
	view, ok := parseInventoryView(w, r, inventoryViewCompact)
	if !ok {
		return
	}
	records, err := a.store.ListDevices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	children := childNamesByParent(records)
	switch view {
	case inventoryViewCompact:
		items := make([]shared.DeviceInventoryCompact, 0, len(records))
		for _, record := range records {
			items = append(items, InventoryCompactFromRecord(record))
		}
		a.writeJSON(w, http.StatusOK, items)
	case inventoryViewInfo:
		items := make([]shared.DeviceInventoryInfo, 0, len(records))
		for _, record := range records {
			managed, err := a.ManagedAccountConfig(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			items = append(items, InventoryInfoFromRecordWithManagedUser(record, children[record.Device.ID], managed.ManagedUser))
		}
		a.writeJSON(w, http.StatusOK, items)
	default:
		items := make([]shared.DeviceInventoryItem, 0, len(records))
		for _, record := range records {
			managed, err := a.ManagedAccountConfig(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			items = append(items, InventoryItemFromRecordWithManagedUser(record, children[record.Device.ID], managed.ManagedUser))
		}
		a.writeJSON(w, http.StatusOK, items)
	}
}

func (a *App) handleDeviceInventory(w http.ResponseWriter, r *http.Request) {
	view, ok := parseInventoryView(w, r, inventoryViewInfo)
	if !ok {
		return
	}
	record, err := a.store.GetDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	records, err := a.store.ListDevices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	children := childNamesByParent(records)
	a.writeInventoryByView(r.Context(), w, view, record, children[record.Device.ID])
}

func (a *App) handleDeviceFind(w http.ResponseWriter, r *http.Request) {
	view, ok := parseInventoryView(w, r, inventoryViewInfo)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	records, err := a.store.ListDevices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	matches := findMatchingDevices(records, query)
	if len(matches) == 0 {
		http.NotFound(w, r)
		return
	}
	if len(matches) > 1 {
		conflict := shared.DeviceFindConflict{Query: query}
		for _, record := range matches {
			conflict.Matches = append(conflict.Matches, FindMatchFromRecord(record))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(conflict)
		return
	}
	children := childNamesByParent(records)
	a.writeInventoryByView(r.Context(), w, view, matches[0], children[matches[0].Device.ID])
}

func (a *App) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	script := `#!/usr/bin/env bash
set -Eeuo pipefail

SERVER_URL="` + a.baseURL(r) + `"
BOOTSTRAP_TOKEN="` + token + `"
TMP_BIN="$(mktemp /tmp/insylus-agent.XXXXXX)"
: "${INSYLUS_AGENT_BIN_PATH:=/usr/local/bin/insylus-agent}"
: "${INSYLUS_AGENT_CONFIG_PATH:=/etc/insylus-agent/config.json}"
: "${INSYLUS_AGENT_SERVICE_NAME:=insylus-agent.service}"
: "${INSYLUS_AGENT_UNIT_PATH:=/etc/systemd/system/$INSYLUS_AGENT_SERVICE_NAME}"
OS_NAME="$(uname -s | tr '[:upper:]' '[:lower:]')"
MACHINE="$(uname -m)"
ARCH=""

log_step() {
  printf '==> %s\n' "$1"
}

case "$MACHINE" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  armv7l|armv7|armhf)
    ARCH="armv7"
    ;;
  *)
    printf 'Unsupported architecture: %s\n' "$MACHINE" >&2
    exit 1
    ;;
esac

trap 'rc=$?; if [ "$rc" -ne 0 ]; then printf "Install failed with exit code %s\n" "$rc" >&2; fi; rm -f "$TMP_BIN"' EXIT

log_step "Preparing Insylus agent installation"
mkdir -p "$(dirname "$INSYLUS_AGENT_BIN_PATH")" "$(dirname "$INSYLUS_AGENT_CONFIG_PATH")"
log_step "Downloading agent from Insylus for $OS_NAME/$ARCH"
curl -fsSL "$SERVER_URL/downloads/insylus-agent?goos=$OS_NAME&goarch=$ARCH" -o "$TMP_BIN"
chmod +x "$TMP_BIN"
log_step "Running installer"
INSYLUS_AGENT_BIN_PATH="$INSYLUS_AGENT_BIN_PATH" \
INSYLUS_AGENT_CONFIG_PATH="$INSYLUS_AGENT_CONFIG_PATH" \
INSYLUS_AGENT_SERVICE_NAME="$INSYLUS_AGENT_SERVICE_NAME" \
INSYLUS_AGENT_UNIT_PATH="$INSYLUS_AGENT_UNIT_PATH" \
  "$TMP_BIN" install --server "$SERVER_URL" --bootstrap-token "$BOOTSTRAP_TOKEN"
`
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func (a *App) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	if a.cfg.AgentBinaryPath == "" {
		http.Error(w, "agent binary path not configured", http.StatusNotFound)
		return
	}
	agentPath, err := resolveAgentBinaryPath(a.cfg.AgentBinaryPath, r.URL.Query().Get("goos"), r.URL.Query().Get("goarch"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	file, err := os.Open(agentPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="insylus-agent"`)
	_, _ = io.Copy(w, file)
}

func resolveAgentBinaryPath(defaultPath, goos, goarch string) (string, error) {
	if goos == "" || goarch == "" {
		return defaultPath, nil
	}
	baseDir := filepath.Dir(defaultPath)
	candidate := filepath.Join(baseDir, fmt.Sprintf("insylus-agent-%s-%s", goos, goarch))
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("no agent binary available for %s/%s", goos, goarch)
}

func (a *App) agentUpdateManifest(r *http.Request, device shared.Device) (shared.AgentUpdateManifest, error) {
	goos := strings.TrimSpace(r.URL.Query().Get("goos"))
	goarch := strings.TrimSpace(r.URL.Query().Get("goarch"))
	manifest := shared.AgentUpdateManifest{
		ServerAgentVersion: version.AgentVersion,
		GOOS:               goos,
		GOARCH:             goarch,
		Status:             shared.AgentUpdateStatusIdle,
	}
	globalEnabled, err := a.store.AgentAutoUpdateDefault(r.Context())
	if err != nil {
		return manifest, err
	}
	record, err := a.store.GetDevice(r.Context(), device.ID)
	if err != nil {
		return manifest, err
	}
	enabled := effectiveAgentAutoUpdate(globalEnabled, record.Update.Override)
	available := compareVersions(device.AgentVersion, version.AgentVersion) < 0
	status := shared.AgentUpdateStatusIdle
	errText := ""
	if available {
		status = shared.AgentUpdateStatusAvailable
	}
	if enabled && (goos == "" || goarch == "") {
		status = shared.AgentUpdateStatusUnsupported
		errText = "agent did not provide goos/goarch"
		enabled = false
	} else if enabled && a.cfg.AgentBinaryPath == "" {
		status = shared.AgentUpdateStatusUnsupported
		errText = "agent binary path not configured"
		enabled = false
	} else if enabled {
		agentPath, err := resolveAgentBinaryPath(a.cfg.AgentBinaryPath, goos, goarch)
		if err != nil {
			status = shared.AgentUpdateStatusUnsupported
			errText = err.Error()
			enabled = false
		} else {
			sum, err := fileSHA256(agentPath)
			if err != nil {
				return manifest, err
			}
			manifest.DownloadURL = a.baseURL(r) + "/downloads/insylus-agent?goos=" + url.QueryEscape(goos) + "&goarch=" + url.QueryEscape(goarch)
			manifest.SHA256 = sum
		}
	}
	manifest.Enabled = enabled
	manifest.Status = status
	manifest.Error = errText
	if err := a.store.RecordAgentUpdateCheck(r.Context(), device.ID, enabled, available, version.AgentVersion, string(status), errText, goos, goarch); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func compareVersions(a, b string) int {
	ap := parseVersionParts(a)
	bp := parseVersionParts(b)
	maxLen := len(ap)
	if len(bp) > maxLen {
		maxLen = len(bp)
	}
	for i := 0; i < maxLen; i++ {
		var av, bv int
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parseVersionParts(v string) []int {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" {
		return nil
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (a *App) writeInventoryByView(ctx context.Context, w http.ResponseWriter, view inventoryView, record DeviceRecord, children []string) {
	switch view {
	case inventoryViewCompact:
		a.writeJSON(w, http.StatusOK, InventoryCompactFromRecord(record))
	case inventoryViewInfo:
		managed, err := a.ManagedAccountConfig(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.writeJSON(w, http.StatusOK, InventoryInfoFromRecordWithManagedUser(record, children, managed.ManagedUser))
	default:
		managed, err := a.ManagedAccountConfig(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.writeJSON(w, http.StatusOK, InventoryItemFromRecordWithManagedUser(record, children, managed.ManagedUser))
	}
}

func parseInventoryView(w http.ResponseWriter, r *http.Request, defaultView inventoryView) (inventoryView, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("view"))
	if raw == "" {
		return defaultView, true
	}
	switch inventoryView(raw) {
	case inventoryViewCompact, inventoryViewInfo, inventoryViewFull:
		return inventoryView(raw), true
	default:
		http.Error(w, "invalid view", http.StatusBadRequest)
		return "", false
	}
}

func findMatchingDevices(records []DeviceRecord, query string) []DeviceRecord {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	var matches []DeviceRecord
	for _, record := range records {
		if strings.EqualFold(record.Device.ID, query) ||
			strings.EqualFold(record.Device.Name, query) ||
			strings.EqualFold(record.Device.Hostname, query) ||
			hasIP(record.Device.IPs, query) {
			matches = append(matches, record)
		}
	}
	return matches
}

func hasIP(ips []string, query string) bool {
	for _, ip := range ips {
		if ip == query {
			return true
		}
	}
	return false
}

func (a *App) authenticateAgentRequest(w http.ResponseWriter, r *http.Request) (shared.Device, bool) {
	token, ok := bearerToken(r)
	if !ok {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return shared.Device{}, false
	}
	device, err := a.store.AuthenticateAgent(r.Context(), token)
	if err != nil {
		http.Error(w, "invalid agent token", http.StatusUnauthorized)
		return shared.Device{}, false
	}
	return device, true
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(header, "Bearer "), true
}
