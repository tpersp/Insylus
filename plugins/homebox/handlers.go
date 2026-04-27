package homebox

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"insylus/internal/httpx"
)

type runtime struct {
	store  store
	render func(http.ResponseWriter, string, any)
}

type configRequest struct {
	BaseURL  string `json:"base_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type pageData struct {
	Config configSummary
}

func (rt runtime) handlePage(w http.ResponseWriter, r *http.Request) {
	summary, err := rt.store.summary(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "homebox.html", pageData{Config: summary})
}

func (rt runtime) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	summary, err := rt.store.summary(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

func (rt runtime) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	req, ok := configFromRequest(w, r)
	if !ok {
		return
	}
	summary, err := rt.store.setConfig(r.Context(), config{
		BaseURL:  req.BaseURL,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, testErr := rt.testConnection(r.Context())
	if testErr != nil {
		_ = rt.store.markError(r.Context(), result.Message)
		if wantsHTML(r) {
			http.Redirect(w, r, "/homebox", http.StatusSeeOther)
			return
		}
		httpx.WriteJSON(w, http.StatusBadGateway, result)
		return
	}
	summary, _ = rt.store.summary(r.Context())
	if wantsHTML(r) {
		http.Redirect(w, r, "/homebox", http.StatusSeeOther)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"config": summary, "test": result})
}

func (rt runtime) handleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	if err := rt.store.deleteConfig(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/homebox", http.StatusSeeOther)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (rt runtime) handleTest(w http.ResponseWriter, r *http.Request) {
	result, err := rt.testConnection(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadGateway, result)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (rt runtime) handleSelf(w http.ResponseWriter, r *http.Request) {
	var out any
	if !rt.requestHomeBox(w, r, "/v1/users/self", &out) {
		return
	}
	rt.writeHomeBoxJSON(w, r, "self", out)
}

func (rt runtime) handleItems(w http.ResponseWriter, r *http.Request) {
	query := normalizedItemsQuery(r.URL.Query())
	path := "/v1/items"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out any
	if !rt.requestHomeBox(w, r, path, &out) {
		return
	}
	rt.writeHomeBoxJSON(w, r, "items", out)
}

func (rt runtime) handleItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	var out any
	if !rt.requestHomeBoxAny(w, r, []string{"/v1/entities/" + url.PathEscape(id), "/v1/items/" + url.PathEscape(id)}, &out) {
		return
	}
	rt.writeHomeBoxJSON(w, r, "item", out)
}

func (rt runtime) handleLabels(w http.ResponseWriter, r *http.Request) {
	var out any
	if !rt.requestHomeBoxAny(w, r, []string{"/v1/tags", "/v1/labels"}, &out) {
		return
	}
	rt.writeHomeBoxJSON(w, r, "tags", out)
}

func (rt runtime) handleLocations(w http.ResponseWriter, r *http.Request) {
	var out any
	if !rt.requestHomeBoxAny(w, r, []string{"/v1/entities?isLocation=true&pageSize=1000", "/v1/locations"}, &out) {
		return
	}
	rt.writeHomeBoxJSON(w, r, "locations", out)
}

func (rt runtime) handleStatistics(w http.ResponseWriter, r *http.Request) {
	var out any
	if !rt.requestHomeBox(w, r, "/v1/groups/statistics", &out) {
		return
	}
	rt.writeHomeBoxJSON(w, r, "statistics", out)
}

func (rt runtime) writeHomeBoxJSON(w http.ResponseWriter, r *http.Request, kind string, payload any) {
	view, ok := parseHomeBoxView(w, r)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, shapeHomeBoxPayload(kind, view, payload))
}

func (rt runtime) testConnection(ctx context.Context) (connectionTestResult, error) {
	client, err := rt.client(ctx)
	if err != nil {
		return connectionTestResult{Status: "error", Message: classifyError(err)}, err
	}
	var self any
	if err := client.GetJSON(ctx, "/v1/users/self", &self); err != nil {
		msg := classifyError(err)
		_ = rt.store.markError(ctx, msg)
		return connectionTestResult{Status: "error", Message: msg}, err
	}
	if err := rt.store.markConnected(ctx); err != nil {
		return connectionTestResult{Status: "error", Message: err.Error()}, err
	}
	return connectionTestResult{Status: "connected", User: self}, nil
}

func (rt runtime) requestHomeBox(w http.ResponseWriter, r *http.Request, path string, out any) bool {
	return rt.requestHomeBoxAny(w, r, []string{path}, out)
}

func (rt runtime) requestHomeBoxAny(w http.ResponseWriter, r *http.Request, paths []string, out any) bool {
	client, err := rt.client(r.Context())
	if err != nil {
		http.Error(w, classifyError(err), http.StatusBadRequest)
		return false
	}
	var lastErr error
	for _, path := range paths {
		err := client.GetJSON(r.Context(), path, out)
		if err == nil {
			_ = rt.store.markConnected(r.Context())
			return true
		}
		lastErr = err
		if !isHomeBoxNotFound(err) {
			break
		}
	}
	if lastErr != nil {
		msg := classifyError(lastErr)
		_ = rt.store.markError(r.Context(), msg)
		http.Error(w, msg, statusForError(lastErr))
		return false
	}
	http.Error(w, "HomeBox API path is required", http.StatusInternalServerError)
	return false
}

func (rt runtime) client(ctx context.Context) (*Client, error) {
	cfg, err := rt.store.getConfig(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("HomeBox is not configured")
		}
		return nil, err
	}
	return NewClient(cfg, func(state authState) error {
		return rt.store.updateAuthState(ctx, state)
	})
}

func configFromRequest(w http.ResponseWriter, r *http.Request) (configRequest, bool) {
	var req configRequest
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if !httpx.DecodeJSON(w, r, &req) {
			return req, false
		}
		return req, true
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return req, false
	}
	req.BaseURL = strings.TrimSpace(r.FormValue("base_url"))
	req.Username = strings.TrimSpace(r.FormValue("username"))
	req.Password = r.FormValue("password")
	return req, true
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "Cannot reach HomeBox") {
		return "Cannot reach HomeBox"
	}
	if errors.Is(err, errHomeBoxAuth) || strings.Contains(msg, "Invalid credentials") {
		return "Invalid credentials"
	}
	if strings.Contains(msg, "Unexpected API response") {
		return "Unexpected API response"
	}
	if strings.Contains(msg, "Auth failed") {
		return "Auth failed"
	}
	return msg
}

func statusForError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := classifyError(err)
	switch msg {
	case "Cannot reach HomeBox":
		return http.StatusBadGateway
	case "Invalid credentials", "Auth failed":
		return http.StatusUnauthorized
	case "Unexpected API response":
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}

func isHomeBoxNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HomeBox API returned 404")
}

func normalizedItemsQuery(query url.Values) url.Values {
	out := url.Values{}
	assetID := ""
	for key, values := range query {
		if strings.EqualFold(key, "view") {
			continue
		}
		if strings.EqualFold(key, "asset_id") || strings.EqualFold(key, "assetId") {
			if len(values) > 0 {
				assetID = strings.TrimSpace(values[0])
			}
			continue
		}
		for _, value := range values {
			out.Add(key, value)
		}
	}
	if assetID != "" {
		if strings.HasPrefix(assetID, "#") {
			out.Set("q", assetID)
		} else {
			out.Set("q", "#"+assetID)
		}
	}
	return out
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html") ||
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
}
