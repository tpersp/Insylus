package httpapi

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"

	"insylus/internal/model"
	"insylus/internal/store"
)

//go:embed static/*.svg
var embeddedStatic embed.FS

type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

type dashboardRow struct {
	ServerName  string
	Hostname    string
	Address     string
	ServerNotes string
	Grants      []dashboardGrant
	HasAccess   bool
}

type dashboardGrant struct {
	PrincipalName  string
	PrincipalKind  string
	PrincipalNotes string
	Account        string
	Sudo           string
	Notes          string
}

type pageData struct {
	Rows       []dashboardRow
	Servers    []model.Server
	Principals []model.Principal
	Grants     []model.AccessGrant
}

func New(st *store.Store) *Server {
	s := &Server{store: st, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err == nil {
		s.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/manage", s.handleManage)
	s.mux.HandleFunc("/servers", s.handleServerForm)
	s.mux.HandleFunc("/servers/update", s.handleServerUpdateForm)
	s.mux.HandleFunc("/servers/delete", s.handleServerDeleteForm)
	s.mux.HandleFunc("/principals", s.handlePrincipalForm)
	s.mux.HandleFunc("/principals/update", s.handlePrincipalUpdateForm)
	s.mux.HandleFunc("/principals/delete", s.handlePrincipalDeleteForm)
	s.mux.HandleFunc("/access", s.handleAccessForm)
	s.mux.HandleFunc("/access/update", s.handleAccessUpdateForm)
	s.mux.HandleFunc("/access/delete", s.handleAccessDeleteForm)
	s.mux.HandleFunc("/api/servers", s.handleServers)
	s.mux.HandleFunc("/api/principals", s.handlePrincipals)
	s.mux.HandleFunc("/api/access", s.handleAccess)
}

func (s *Server) handleServerForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err := s.store.CreateServer(r.Context(), model.Server{
		Name:     r.FormValue("name"),
		Hostname: r.FormValue("hostname"),
		Address:  r.FormValue("address"),
		Notes:    r.FormValue("notes"),
	})
	redirectOrError(w, r, err)
}

func (s *Server) handleServerUpdateForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := parseID(r.FormValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = s.store.UpdateServer(r.Context(), model.Server{
		ID:       id,
		Name:     r.FormValue("name"),
		Hostname: r.FormValue("hostname"),
		Address:  r.FormValue("address"),
		Notes:    r.FormValue("notes"),
	})
	redirectOrError(w, r, err)
}

func (s *Server) handleServerDeleteForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := parseID(r.FormValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectOrError(w, r, s.store.DeleteServer(r.Context(), id))
}

func (s *Server) handlePrincipalForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err := s.store.CreatePrincipal(r.Context(), model.Principal{
		Name:  r.FormValue("name"),
		Kind:  r.FormValue("kind"),
		Notes: r.FormValue("notes"),
	})
	redirectOrError(w, r, err)
}

func (s *Server) handlePrincipalUpdateForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := parseID(r.FormValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = s.store.UpdatePrincipal(r.Context(), model.Principal{
		ID:    id,
		Name:  r.FormValue("name"),
		Kind:  r.FormValue("kind"),
		Notes: r.FormValue("notes"),
	})
	redirectOrError(w, r, err)
}

func (s *Server) handlePrincipalDeleteForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := parseID(r.FormValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectOrError(w, r, s.store.DeletePrincipal(r.Context(), id))
}

func (s *Server) handleAccessForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err := s.store.CreateAccessGrant(r.Context(), model.AccessGrant{
		ServerName:    r.FormValue("server"),
		PrincipalName: r.FormValue("principal"),
		Account:       r.FormValue("account"),
		Sudo:          r.FormValue("sudo"),
		Notes:         r.FormValue("notes"),
	})
	redirectOrError(w, r, err)
}

func (s *Server) handleAccessUpdateForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := parseID(r.FormValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = s.store.UpdateAccessGrant(r.Context(), model.AccessGrant{
		ID:            id,
		ServerName:    r.FormValue("server"),
		PrincipalName: r.FormValue("principal"),
		Account:       r.FormValue("account"),
		Sudo:          r.FormValue("sudo"),
		Notes:         r.FormValue("notes"),
	})
	redirectOrError(w, r, err)
}

func (s *Server) handleAccessDeleteForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := parseID(r.FormValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectOrError(w, r, s.store.DeleteAccessGrant(r.Context(), id))
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}

func redirectOrError(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/manage", http.StatusSeeOther)
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListServers(r.Context())
		writeResult(w, items, err)
	case http.MethodPost:
		var in model.Server
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.store.CreateServer(r.Context(), in)
		writeCreated(w, item, err)
	case http.MethodPut:
		var in model.Server
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.store.UpdateServer(r.Context(), in)
		writeResult(w, item, err)
	case http.MethodDelete:
		id, err := idFromRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeDeleted(w, s.store.DeleteServer(r.Context(), id))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePrincipals(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListPrincipals(r.Context())
		writeResult(w, items, err)
	case http.MethodPost:
		var in model.Principal
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.store.CreatePrincipal(r.Context(), in)
		writeCreated(w, item, err)
	case http.MethodPut:
		var in model.Principal
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.store.UpdatePrincipal(r.Context(), in)
		writeResult(w, item, err)
	case http.MethodDelete:
		id, err := idFromRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeDeleted(w, s.store.DeletePrincipal(r.Context(), id))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAccess(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListAccessGrants(r.Context())
		writeResult(w, items, err)
	case http.MethodPost:
		var in model.AccessGrant
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.store.CreateAccessGrant(r.Context(), in)
		writeCreated(w, item, err)
	case http.MethodPut:
		var in model.AccessGrant
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.store.UpdateAccessGrant(r.Context(), in)
		writeResult(w, item, err)
	case http.MethodDelete:
		id, err := idFromRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeDeleted(w, s.store.DeleteAccessGrant(r.Context(), id))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func idFromRequest(r *http.Request) (int64, error) {
	return parseID(r.URL.Query().Get("id"))
}

func writeResult(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func writeDeleted(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeCreated(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := s.loadPageData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardTemplate.Execute(w, data)
}

func (s *Server) handleManage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/manage" {
		http.NotFound(w, r)
		return
	}
	data, err := s.loadPageData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = manageTemplate.Execute(w, data)
}

func (s *Server) loadPageData(r *http.Request) (pageData, error) {
	servers, err := s.store.ListServers(r.Context())
	if err != nil {
		return pageData{}, err
	}
	principals, err := s.store.ListPrincipals(r.Context())
	if err != nil {
		return pageData{}, err
	}
	grants, err := s.store.ListAccessGrants(r.Context())
	if err != nil {
		return pageData{}, err
	}
	return pageData{
		Rows:       buildDashboardRows(servers, principals, grants),
		Servers:    servers,
		Principals: principals,
		Grants:     grants,
	}, nil
}

func buildDashboardRows(servers []model.Server, principals []model.Principal, grants []model.AccessGrant) []dashboardRow {
	rows := make([]dashboardRow, 0, len(servers))
	rowByServer := make(map[string]int, len(servers))
	for _, server := range servers {
		rowByServer[server.Name] = len(rows)
		rows = append(rows, dashboardRow{
			ServerName:  server.Name,
			Hostname:    server.Hostname,
			Address:     server.Address,
			ServerNotes: server.Notes,
		})
	}
	principalByName := make(map[string]model.Principal, len(principals))
	for _, principal := range principals {
		principalByName[principal.Name] = principal
	}
	for _, grant := range grants {
		principal := principalByName[grant.PrincipalName]
		i, ok := rowByServer[grant.ServerName]
		if !ok {
			continue
		}
		rows[i].HasAccess = true
		rows[i].Grants = append(rows[i].Grants, dashboardGrant{
			PrincipalName:  grant.PrincipalName,
			PrincipalKind:  principal.Kind,
			PrincipalNotes: principal.Notes,
			Account:        grant.Account,
			Sudo:           grant.Sudo,
			Notes:          grant.Notes,
		})
	}
	return rows
}

const baseStyle = `
:root { color-scheme: dark; --bg:#0f1419; --ink:#e5edf5; --muted:#93a2b3; --line:#263241; --panel:#161d25; --panel2:#1c2530; --field:#0c1117; --accent:#14b8a6; --warn:#f59e0b; --danger:#f87171; --ok:#34d399; }
* { box-sizing: border-box; }
body { margin:0; font:14px/1.45 system-ui,-apple-system,Segoe UI,sans-serif; color:var(--ink); background:var(--bg); }
header { padding:18px 24px; border-bottom:1px solid var(--line); display:flex; align-items:center; justify-content:space-between; gap:18px; background:#111820; }
.brand { display:flex; align-items:center; gap:12px; min-width:0; }
.brand-mark { width:38px; height:38px; flex:0 0 auto; }
.brand-text { display:grid; gap:1px; }
h1 { margin:0; font-size:22px; letter-spacing:0; }
h2 { margin:0 0 10px; font-size:16px; letter-spacing:0; }
nav { display:flex; gap:8px; }
nav a { color:var(--muted); text-decoration:none; border:1px solid var(--line); border-radius:6px; padding:7px 10px; }
nav a.active { color:#06201d; background:var(--accent); border-color:var(--accent); font-weight:700; }
main { padding:20px 24px 32px; display:grid; gap:20px; }
.summary { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:12px; max-width:820px; }
.metric { border:1px solid var(--line); border-radius:8px; padding:12px; background:var(--panel); }
.metric strong { display:block; font-size:24px; }
.metric span, header span, .empty { color:var(--muted); }
section { min-width:0; }
.dashboard { overflow:auto; border:1px solid var(--line); border-radius:8px; background:var(--panel); }
.dashboard table { width:100%; border-collapse:collapse; min-width:860px; }
.dashboard th,.dashboard td { padding:10px 12px; border-bottom:1px solid var(--line); text-align:left; vertical-align:middle; }
.dashboard th { color:var(--muted); font-size:12px; font-weight:700; background:#121a23; position:sticky; top:0; }
.dashboard tr:last-child td { border-bottom:0; }
.dashboard .primary { font-weight:700; }
.dashboard .secondary { display:block; color:var(--muted); font-size:12px; margin-top:2px; }
.badge { display:inline-flex; align-items:center; min-height:24px; padding:2px 8px; border:1px solid var(--line); border-radius:999px; background:var(--field); color:var(--muted); font-size:12px; font-weight:700; }
.badge.ai-agent { color:var(--accent); border-color:#155e59; }
.badge.human { color:#93c5fd; border-color:#1d4ed8; }
.badge.service { color:#c4b5fd; border-color:#6d28d9; }
.grant-list { display:flex; flex-wrap:wrap; gap:8px; }
.grant-chip { border:1px solid var(--line); border-radius:8px; background:var(--field); padding:8px; display:grid; gap:5px; min-width:180px; }
.grant-chip strong { font-size:13px; }
.grant-meta { display:flex; flex-wrap:wrap; gap:6px; align-items:center; color:var(--muted); font-size:12px; }
.sudo { font-weight:700; }
.sudo.passwordless { color:var(--danger); }
.sudo.prompted { color:var(--warn); }
.sudo.none { color:var(--ok); }
.grid { display:grid; grid-template-columns:1fr 1fr; gap:20px; }
.forms { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:14px; align-items:start; }
form { border:1px solid var(--line); border-radius:8px; padding:12px; background:var(--panel); display:grid; gap:8px; }
label { display:grid; gap:3px; color:var(--muted); font-size:12px; }
input,select { width:100%; border:1px solid var(--line); border-radius:6px; padding:8px; font:inherit; background:var(--field); color:var(--ink); }
input:focus,select:focus { outline:2px solid color-mix(in srgb, var(--accent) 45%, transparent); border-color:var(--accent); }
button { border:0; border-radius:6px; padding:9px 10px; font:inherit; font-weight:700; color:#06201d; background:var(--accent); cursor:pointer; }
.button-row { display:flex; gap:8px; align-items:center; }
.button-row button { flex:1; }
.delete-form { padding:0; border:0; background:transparent; display:block; }
.delete-form button,.danger-button { background:transparent; color:var(--danger); border:1px solid #7f1d1d; }
.edit-list { display:grid; gap:8px; }
.edit-row { display:grid; gap:8px; grid-template-columns:repeat(4,minmax(0,1fr)) auto; align-items:end; background:var(--panel2); }
.edit-row.server { grid-template-columns:repeat(4,minmax(0,1fr)) 148px; }
.edit-row.principal { grid-template-columns:1fr 150px 1fr 148px; }
.edit-row button { min-width:72px; }
code { font-family:ui-monospace,SFMono-Regular,Consolas,monospace; font-size:12px; }
@media (max-width: 760px) {
	header { padding:16px; display:grid; gap:12px; }
	main { padding:16px; }
	.summary,.grid,.forms,.edit-row,.edit-row.server,.edit-row.principal { grid-template-columns:1fr; }
	nav { width:100%; }
	nav a { flex:1; text-align:center; }
}
`

func pageStart(title, active string) string {
	dashboardActive := ""
	manageActive := ""
	if active == "dashboard" {
		dashboardActive = "active"
	}
	if active == "manage" {
		manageActive = "active"
	}
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<link rel="icon" type="image/svg+xml" href="/static/insylus-favicon.svg">
<link rel="shortcut icon" href="/static/insylus-favicon.svg">
<style>` + baseStyle + `</style>
</head>
<body>
<header><div class="brand"><img class="brand-mark" src="/static/insylus-icon.svg" alt="" width="38" height="38"><div class="brand-text"><h1>Insylus</h1><span>server access inventory</span></div></div><nav><a class="` + dashboardActive + `" href="/">Dashboard</a><a class="` + manageActive + `" href="/manage">Manage</a></nav></header>
<main>`
}

const pageEnd = `</main>
</body>
</html>`

var dashboardTemplate = template.Must(template.New("dashboard").Parse(pageStart("Insylus", "dashboard") + `
<div class="summary">
	<div class="metric"><strong>{{len .Servers}}</strong><span>servers</span></div>
	<div class="metric"><strong>{{len .Principals}}</strong><span>principals</span></div>
	<div class="metric"><strong>{{len .Grants}}</strong><span>access grants</span></div>
</div>

<section>
	<h2>Overview</h2>
	{{if .Rows}}
	<div class="dashboard">
		<table>
			<thead>
				<tr><th>Server</th><th>Address</th><th>Access</th><th>Server notes</th></tr>
			</thead>
			<tbody>
				{{range .Rows}}
				<tr>
					<td><span class="primary">{{.ServerName}}</span>{{if .Hostname}}<span class="secondary">{{.Hostname}}</span>{{end}}</td>
					<td>{{if .Address}}<code>{{.Address}}</code>{{else}}<span class="empty">none</span>{{end}}</td>
					<td>{{if .HasAccess}}<div class="grant-list">{{range .Grants}}<div class="grant-chip"><div><strong>{{.PrincipalName}}</strong> {{if .PrincipalKind}}<span class="badge {{.PrincipalKind}}">{{.PrincipalKind}}</span>{{end}}</div><div class="grant-meta"><code>{{.Account}}</code><span class="sudo {{.Sudo}}">{{.Sudo}}</span></div>{{if .PrincipalNotes}}<span class="secondary">{{.PrincipalNotes}}</span>{{end}}{{if .Notes}}<span class="secondary">{{.Notes}}</span>{{end}}</div>{{end}}</div>{{else}}<span class="empty">no access recorded</span>{{end}}</td>
					<td>{{.ServerNotes}}</td>
				</tr>
				{{end}}
			</tbody>
		</table>
	</div>
	{{else}}<p class="empty">No inventory yet.</p>{{end}}
</section>
` + pageEnd))

var manageTemplate = template.Must(template.New("manage").Parse(pageStart("Insylus Manage", "manage") + `
<section>
	<h2>Add</h2>
	<div class="forms">
		<form method="post" action="/servers">
			<label>Name <input name="name" required autocomplete="off"></label>
			<label>Hostname <input name="hostname" autocomplete="off"></label>
			<label>Address <input name="address" autocomplete="off"></label>
			<label>Notes <input name="notes" autocomplete="off"></label>
			<button type="submit">Add server</button>
		</form>
		<form method="post" action="/principals">
			<label>Name <input name="name" required autocomplete="off"></label>
			<label>Kind <select name="kind"><option value="ai-agent">AI agent</option><option value="human">Human</option><option value="service">Service</option></select></label>
			<label>Notes <input name="notes" autocomplete="off"></label>
			<button type="submit">Add principal</button>
		</form>
		<form method="post" action="/access">
			<label>Server <select name="server" required>{{range .Servers}}<option value="{{.Name}}">{{.Name}}</option>{{end}}</select></label>
			<label>Principal <select name="principal" required>{{range .Principals}}<option value="{{.Name}}">{{.Name}}</option>{{end}}</select></label>
			<label>Account <input name="account" required autocomplete="off"></label>
			<label>Sudo <select name="sudo"><option value="none">None</option><option value="prompted">Prompted</option><option value="passwordless">Passwordless</option></select></label>
			<button type="submit">Add access</button>
		</form>
	</div>
</section>

<section>
	<h2>Access</h2>
	{{if .Grants}}
	<div class="edit-list">
		{{range .Grants}}
		{{$grant := .}}
		<form class="edit-row" method="post" action="/access/update">
			<input type="hidden" name="id" value="{{.ID}}">
			<label>Server <select name="server" required>{{range $.Servers}}<option value="{{.Name}}" {{if eq .Name $grant.ServerName}}selected{{end}}>{{.Name}}</option>{{end}}</select></label>
			<label>Principal <select name="principal" required>{{range $.Principals}}<option value="{{.Name}}" {{if eq .Name $grant.PrincipalName}}selected{{end}}>{{.Name}}</option>{{end}}</select></label>
			<label>Account <input name="account" value="{{.Account}}" required autocomplete="off"></label>
			<label>Sudo <select class="sudo {{.Sudo}}" name="sudo"><option value="none" {{if eq .Sudo "none"}}selected{{end}}>None</option><option value="prompted" {{if eq .Sudo "prompted"}}selected{{end}}>Prompted</option><option value="passwordless" {{if eq .Sudo "passwordless"}}selected{{end}}>Passwordless</option></select></label>
			<div class="button-row"><button type="submit">Save</button><button class="danger-button" type="submit" form="delete-access-{{.ID}}">Delete</button></div>
		</form>
		<form id="delete-access-{{.ID}}" class="delete-form" method="post" action="/access/delete"><input type="hidden" name="id" value="{{.ID}}"></form>
		{{end}}
	</div>
	{{else}}<p class="empty">No access grants yet.</p>{{end}}
</section>

<div class="grid">
<section>
	<h2>Servers</h2>
	{{if .Servers}}
	<div class="edit-list">
		{{range .Servers}}
		<form class="edit-row server" method="post" action="/servers/update">
			<input type="hidden" name="id" value="{{.ID}}">
			<label>Name <input name="name" value="{{.Name}}" required autocomplete="off"></label>
			<label>Hostname <input name="hostname" value="{{.Hostname}}" autocomplete="off"></label>
			<label>Address <input name="address" value="{{.Address}}" autocomplete="off"></label>
			<label>Notes <input name="notes" value="{{.Notes}}" autocomplete="off"></label>
			<div class="button-row"><button type="submit">Save</button><button class="danger-button" type="submit" form="delete-server-{{.ID}}">Delete</button></div>
		</form>
		<form id="delete-server-{{.ID}}" class="delete-form" method="post" action="/servers/delete"><input type="hidden" name="id" value="{{.ID}}"></form>
		{{end}}
	</div>
	{{else}}<p class="empty">No servers yet.</p>{{end}}
</section>

<section>
	<h2>Principals</h2>
	{{if .Principals}}
	<div class="edit-list">
		{{range .Principals}}
		<form class="edit-row principal" method="post" action="/principals/update">
			<input type="hidden" name="id" value="{{.ID}}">
			<label>Name <input name="name" value="{{.Name}}" required autocomplete="off"></label>
			<label>Kind <select name="kind"><option value="ai-agent" {{if eq .Kind "ai-agent"}}selected{{end}}>AI agent</option><option value="human" {{if eq .Kind "human"}}selected{{end}}>Human</option><option value="service" {{if eq .Kind "service"}}selected{{end}}>Service</option></select></label>
			<label>Notes <input name="notes" value="{{.Notes}}" autocomplete="off"></label>
			<div class="button-row"><button type="submit">Save</button><button class="danger-button" type="submit" form="delete-principal-{{.ID}}">Delete</button></div>
		</form>
		<form id="delete-principal-{{.ID}}" class="delete-form" method="post" action="/principals/delete"><input type="hidden" name="id" value="{{.ID}}"></form>
		{{end}}
	</div>
	{{else}}<p class="empty">No principals yet.</p>{{end}}
</section>
</div>
` + pageEnd))
