package discovery

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"insylus/internal/pluginhost"
)

type runtime struct {
	store   store
	scanner scanner
	render  func(http.ResponseWriter, string, any)
}

type pageData struct {
	Candidates   []candidate
	DefaultPorts string
}

func newRuntime(host pluginhost.Host) runtime {
	return runtime{
		store:   newStore(host),
		scanner: lanScanner{},
		render:  host.Web().Render,
	}
}

func (rt runtime) handlePage(w http.ResponseWriter, r *http.Request) {
	items, err := rt.store.listCandidates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defaultPortsText := make([]string, 0, len(defaultPorts))
	for _, port := range defaultPorts {
		defaultPortsText = append(defaultPortsText, strconv.Itoa(port))
	}
	rt.render(w, "discovery.html", pageData{
		Candidates:   items,
		DefaultPorts: strings.Join(defaultPortsText, ", "),
	})
}

func (rt runtime) handleListAPI(w http.ResponseWriter, r *http.Request) {
	items, err := rt.store.listCandidates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (rt runtime) handleScan(w http.ResponseWriter, r *http.Request) {
	req, ok := scanRequestFromHTTP(w, r)
	if !ok {
		return
	}
	scan, err := rt.scanner.ScanSubnet(r.Context(), req.CIDR, req.Ports)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	items, err := rt.store.saveScan(r.Context(), req.CIDR, scan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	scan.Candidates = items

	if wantsHTML(r) {
		http.Redirect(w, r, "/discovery", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, scan)
}

func (rt runtime) handlePromote(w http.ResponseWriter, r *http.Request) {
	id, ok := candidateIDFromPath(w, r)
	if !ok {
		return
	}
	item, target, err := rt.store.promoteCandidate(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/devices/"+target.ID, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, promoteResponse{
		Candidate: item,
		TargetID:  target.ID,
	})
}

func (rt runtime) handleStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := candidateIDFromPath(w, r)
	if !ok {
		return
	}
	status, ok := statusFromHTTP(w, r)
	if !ok {
		return
	}
	item, err := rt.store.setStatus(r.Context(), id, status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/discovery", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func scanRequestFromHTTP(w http.ResponseWriter, r *http.Request) (scanRequest, bool) {
	var req scanRequest
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return req, false
		}
		req.Ports = normalizedPorts(req.Ports)
		req.CIDR = strings.TrimSpace(req.CIDR)
		if req.CIDR == "" {
			http.Error(w, "cidr is required", http.StatusBadRequest)
			return req, false
		}
		return req, true
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return req, false
	}
	req.CIDR = strings.TrimSpace(r.FormValue("cidr"))
	req.Ports = normalizePortsText(r.FormValue("ports"))
	if req.CIDR == "" {
		http.Error(w, "cidr is required", http.StatusBadRequest)
		return req, false
	}
	return req, true
}

func statusFromHTTP(w http.ResponseWriter, r *http.Request) (string, bool) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req statusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return "", false
		}
		return strings.TrimSpace(req.Status), true
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return "", false
	}
	return strings.TrimSpace(r.FormValue("status")), true
}

func candidateIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid candidate id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func wantsHTML(r *http.Request) bool {
	return !strings.HasPrefix(r.URL.Path, "/api/")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func formatSeenSince(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
