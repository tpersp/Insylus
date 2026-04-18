package jellyfin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"insylus/internal/pluginhost"
)

type runtime struct {
	store  store
	render func(http.ResponseWriter, string, any)
}

// jellyfinTokenRequest is the API request body for setting a token.
type jellyfinTokenRequest struct {
	DeviceID        string `json:"device_id"`
	Server          string `json:"server"`
	ServerName      string `json:"server_name"`
	APIURL          string `json:"api_url"`
	APIKey          string `json:"api_key"`
	DefaultUserID   string `json:"default_user_id"`
	DefaultUsername string `json:"default_username"`
	TLSInsecure     bool   `json:"tls_insecure"`
}

func (rt runtime) handleJellyfinServers(w http.ResponseWriter, r *http.Request) {
	servers, err := rt.store.listConfiguredServers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

func (rt runtime) handleJellyfinSetToken(w http.ResponseWriter, r *http.Request) {
	req, ok := rt.jellyfinTokenRequest(w, r)
	if !ok {
		return
	}
	record, existing, err := rt.resolveJellyfinConfigTarget(r, req)
	if err != nil {
		rt.writeJellyfinResolveError(w, r, req.ServerName, err)
		return
	}
	serverName := firstNonEmpty(req.ServerName, existing.ServerName, defaultServerName(record))
	token, err := rt.store.setToken(r.Context(), jellyfinToken{
		DeviceID:        record.ID,
		ServerName:      serverName,
		APIURL:          strings.TrimRight(strings.TrimSpace(req.APIURL), "/"),
		APIKey:          req.APIKey,
		DefaultUserID:   req.DefaultUserID,
		DefaultUsername: req.DefaultUsername,
		TLSInsecure:     req.TLSInsecure,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/jellyfin", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (rt runtime) handleJellyfinDeleteToken(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if deviceID == "" {
		deviceID = strings.TrimSpace(r.FormValue("device_id"))
	}
	if deviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}
	if err := rt.store.deleteToken(r.Context(), deviceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/jellyfin", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (rt runtime) handleJellyfinLibraries(w http.ResponseWriter, r *http.Request) {
	_, _, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	libraries, err := client.GetLibraries(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, libraries)
}

func (rt runtime) handleJellyfinItems(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	parentID := strings.TrimSpace(r.URL.Query().Get("parentId"))
	itemType := strings.TrimSpace(r.URL.Query().Get("type"))
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		userID = token.DefaultUserID
	}

	// Resolve username to user ID if needed
	if userID != "" && !isUUID(userID) {
		user, err := client.GetUser(r.Context(), userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("user %q not found: %v", userID, err), http.StatusBadGateway)
			return
		}
		userID = user.ID
	}

	items, err := client.GetItems(ctxWithUser(r.Context(), userID), parentID, itemType, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Sort by name
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	writeJSON(w, http.StatusOK, items)
}

func (rt runtime) handleJellyfinMovies(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	parentID := strings.TrimSpace(r.URL.Query().Get("parentId"))
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		userID = token.DefaultUserID
	}

	items, err := client.GetItemsByType(ctxWithUser(r.Context(), userID), parentID, "Movie", userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	sortItemsByName(items)
	writeJSON(w, http.StatusOK, items)
}

func (rt runtime) handleJellyfinSeries(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	parentID := strings.TrimSpace(r.URL.Query().Get("parentId"))
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		userID = token.DefaultUserID
	}

	items, err := client.GetItemsByType(ctxWithUser(r.Context(), userID), parentID, "Series", userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	sortItemsByName(items)
	writeJSON(w, http.StatusOK, items)
}

func (rt runtime) handleJellyfinEpisodes(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	parentID := strings.TrimSpace(r.URL.Query().Get("parentId"))
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		userID = token.DefaultUserID
	}

	items, err := client.GetItemsByType(ctxWithUser(r.Context(), userID), parentID, "Episode", userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Sort by series, season, episode
	sort.Slice(items, func(i, j int) bool {
		if items[i].SeriesName != items[j].SeriesName {
			return items[i].SeriesName < items[j].SeriesName
		}
		if items[i].SeasonNumber != items[j].SeasonNumber {
			return items[i].SeasonNumber < items[j].SeasonNumber
		}
		return items[i].EpisodeNumber < items[j].EpisodeNumber
	})
	writeJSON(w, http.StatusOK, items)
}

func (rt runtime) handleJellyfinLatest(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		userID = token.DefaultUserID
	}
	limit := 20

	items, err := client.GetLatestItems(ctxWithUser(r.Context(), userID), userID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (rt runtime) handleJellyfinResume(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		userID = token.DefaultUserID
	}
	if userID == "" {
		http.Error(w, "userId is required for resume items", http.StatusBadRequest)
		return
	}
	// Resolve username to user ID if needed (only when userID looks like a name, not a UUID)
	if !isUUID(userID) {
		user, err := client.GetUser(r.Context(), userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("user %q not found: %v", userID, err), http.StatusBadGateway)
			return
		}
		userID = user.ID
	}

	items, err := client.GetResumeItems(ctxWithUser(r.Context(), userID), userID, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	progress := make([]ItemProgress, 0, len(items))
	for _, item := range items {
		if item.UserData != nil {
			progress = append(progress, ItemProgress{
				ItemID:       item.ID,
				Name:         item.Name,
				SeriesName:   item.SeriesName,
				Type:         item.Type,
				Watched:      item.UserData.Played,
				Progress:     progressPercent(item.UserData.PlaybackPositionTicks, item.RunTimeTicks),
				Position:     FormatRuntime(item.UserData.PlaybackPositionTicks),
				Duration:     FormatRuntime(item.RunTimeTicks),
				RunTimeTicks: item.RunTimeTicks,
			})
		}
	}
	writeJSON(w, http.StatusOK, progress)
}

func (rt runtime) handleJellyfinItem(w http.ResponseWriter, r *http.Request) {
	_, _, client, ok := rt.jellyfinClientForRequest(w, r)
	if !ok {
		return
	}
	itemID := strings.TrimSpace(r.PathValue("item_id"))
	if itemID == "" {
		http.Error(w, "item_id is required", http.StatusBadRequest)
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))

	item, err := client.GetItemWithUserData(ctxWithUser(r.Context(), userID), itemID, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (rt runtime) handleJellyfinPage(w http.ResponseWriter, r *http.Request) {
	servers, err := rt.store.listConfiguredServers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	devices, err := rt.store.inventory.ListDevices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "jellyfin.html", jellyfinPageData{
		Servers: servers,
		Devices: devices,
	})
}

type jellyfinPageData struct {
	Servers []jellyfinTokenSummary
	Devices []pluginhost.InventoryDevice
}

func (rt runtime) jellyfinTokenRequest(w http.ResponseWriter, r *http.Request) (jellyfinTokenRequest, bool) {
	var req jellyfinTokenRequest
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if !decodeJSON(w, r, &req) {
			return req, false
		}
		return req, true
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return req, false
	}
	req.DeviceID = strings.TrimSpace(r.FormValue("device_id"))
	req.Server = strings.TrimSpace(r.FormValue("server"))
	req.ServerName = strings.TrimSpace(r.FormValue("server_name"))
	req.APIURL = strings.TrimSpace(r.FormValue("api_url"))
	req.APIKey = strings.TrimSpace(r.FormValue("api_key"))
	req.DefaultUserID = strings.TrimSpace(r.FormValue("default_user_id"))
	req.DefaultUsername = strings.TrimSpace(r.FormValue("default_username"))
	req.TLSInsecure = r.FormValue("tls_insecure") == "on" || r.FormValue("tls_insecure") == "true"
	return req, true
}

func (rt runtime) resolveJellyfinConfigTarget(r *http.Request, req jellyfinTokenRequest) (pluginhost.InventoryDevice, jellyfinTokenSummary, error) {
	if strings.TrimSpace(req.DeviceID) != "" {
		record, err := rt.store.inventory.GetDevice(r.Context(), req.DeviceID)
		if err != nil {
			return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, err
		}
		summary, err := rt.store.getTokenSummary(r.Context(), req.DeviceID)
		if errors.Is(err, sql.ErrNoRows) {
			summary = jellyfinTokenSummary{
				DeviceID:   record.ID,
				DeviceName: record.Name,
				Hostname:   record.Hostname,
				ServerName: defaultServerName(record),
			}
		} else if err != nil {
			return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, err
		}
		return record, summary, nil
	}
	query := firstNonEmpty(req.Server, req.ServerName, req.APIURL)
	if query != "" {
		if record, summary, err := rt.store.resolveDevice(r.Context(), query); err == nil {
			return record, summary, nil
		}
	}
	name := firstNonEmpty(req.ServerName, req.Server, req.APIURL)
	if name == "" {
		return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, fmt.Errorf("server_name, server, api_url, or device_id is required")
	}
	target, err := rt.store.targets.Create(r.Context(), pluginhost.TargetInput{
		Name:      name,
		Kind:      "jellyfin-server",
		Hostname:  req.Server,
		APIURL:    req.APIURL,
		CreatedBy: "jellyfin",
	})
	if err != nil {
		return pluginhost.InventoryDevice{}, jellyfinTokenSummary{}, err
	}
	record := pluginhost.InventoryDevice{ID: target.ID, Name: target.Name, Hostname: target.Hostname, IPs: target.IPs, DeviceType: target.Kind, Purpose: target.Kind}
	return record, jellyfinTokenSummary{
		DeviceID:   record.ID,
		DeviceName: record.Name,
		Hostname:   record.Hostname,
		ServerName: defaultServerName(record),
	}, nil
}

func (rt runtime) writeJellyfinResolveError(w http.ResponseWriter, r *http.Request, query string, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, fmt.Sprintf("no device found for Jellyfin server %q", query), http.StatusNotFound)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func (rt runtime) jellyfinClientForRequest(w http.ResponseWriter, r *http.Request) (pluginhost.InventoryDevice, jellyfinToken, *JellyfinClient, bool) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if deviceID == "" {
		deviceID = strings.TrimSpace(r.FormValue("device_id"))
	}
	record, err := rt.store.inventory.GetDevice(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return pluginhost.InventoryDevice{}, jellyfinToken{}, nil, false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return pluginhost.InventoryDevice{}, jellyfinToken{}, nil, false
	}
	authToken, err := rt.store.getToken(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "no Jellyfin API key configured for "+record.Name, http.StatusBadRequest)
			return pluginhost.InventoryDevice{}, jellyfinToken{}, nil, false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return pluginhost.InventoryDevice{}, jellyfinToken{}, nil, false
	}
	apiURL := authToken.APIURL
	if apiURL == "" {
		apiURL = defaultJellyfinAPIURL(record)
	}
	client, err := NewJellyfinClient(apiURL, authToken.APIKey, authToken.TLSInsecure)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return pluginhost.InventoryDevice{}, jellyfinToken{}, nil, false
	}
	return record, authToken, client, true
}

func defaultJellyfinAPIURL(record pluginhost.InventoryDevice) string {
	host := ""
	if len(record.IPs) > 0 {
		host = record.IPs[0]
	} else if strings.TrimSpace(record.Hostname) != "" {
		host = record.Hostname
	} else {
		host = record.Name
	}
	return "http://" + host + ":8096"
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html") ||
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func sortItemsByName(items []JellyfinItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
}

func progressPercent(position, duration int64) float64 {
	if duration <= 0 {
		return 0
	}
	posSeconds := position / 10_000_000
	durSeconds := duration / 10_000_000
	if durSeconds == 0 {
		return 0
	}
	return float64(posSeconds) / float64(durSeconds) * 100
}

type contextKey string

const userIDKey contextKey = "jellyfin_user_id"

func ctxWithUser(ctx context.Context, userID string) context.Context {
	if userID == "" {
		return ctx
	}
	return context.WithValue(ctx, userIDKey, userID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isUUID(s string) bool {
	// Jellyfin uses 32-char hex UUIDs (no dashes) or standard 36-char with dashes.
	if len(s) == 36 {
		for i, c := range s {
			if i == 8 || i == 13 || i == 18 || i == 23 {
				if c != '-' {
					return false
				}
			} else {
				if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
					continue
				}
				return false
			}
		}
		return true
	}
	// 32-char hex without dashes (Jellyfin format)
	if len(s) == 32 {
		for _, c := range s {
			if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
				continue
			}
			return false
		}
		return true
	}
	return false
}
