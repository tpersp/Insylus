package topology

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type runtime struct {
	db      pluginhost.DBHost
	targets pluginhost.TargetService
	render  func(http.ResponseWriter, string, any)
}

type pageData struct {
	Options []endpointOption
	Nodes   []manualNode
	Links   []manualLink
}

type endpointOption struct {
	Value string
	Label string
}

type manualNode struct {
	ID   int64
	Name string
	Kind string
	Note string
}

type manualLink struct {
	ID       int64
	FromKind string
	FromID   string
	ToKind   string
	ToID     string
	Label    string
}

type positionUpdate struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

func (rt runtime) handlePage(w http.ResponseWriter, r *http.Request) {
	targets, err := rt.targets.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	nodes, err := rt.listNodes(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	links, err := rt.listLinks(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	options := make([]endpointOption, 0, len(targets)+len(nodes))
	for _, target := range targets {
		options = append(options, endpointOption{Value: "target:" + target.ID, Label: target.Kind + ": " + target.Name})
	}
	for _, node := range nodes {
		options = append(options, endpointOption{Value: fmt.Sprintf("topology_node:%d", node.ID), Label: node.Kind + ": " + node.Name})
	}
	if rt.render == nil {
		http.Error(w, "renderer unavailable", http.StatusInternalServerError)
		return
	}
	rt.render(w, "topology.html", pageData{Options: options, Nodes: nodes, Links: links})
}

func (rt runtime) handleGraph(w http.ResponseWriter, r *http.Request) {
	targets, err := rt.targets.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	graph := shared.TopologyGraph{
		Nodes: []shared.TopologyGraphNode{},
		Links: []shared.TopologyGraphLink{},
	}
	nodeIDs := map[string]struct{}{}
	positions, err := rt.listPositions(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, target := range targets {
		kind := shared.TopologyNodeKindDevice
		switch target.Kind {
		case "vm":
			kind = shared.TopologyNodeKindDevice
		case "container":
			kind = shared.TopologyNodeKindWorkload
		}
		id := "target:" + target.ID
		nodeIDs[id] = struct{}{}
		graph.Nodes = append(graph.Nodes, shared.TopologyGraphNode{
			ID:       id,
			Kind:     kind,
			Label:    target.Name,
			Source:   shared.TopologySourceManual,
			URL:      "/devices/" + target.ID,
			Position: positions[id],
		})
	}
	nodes, err := rt.listNodes(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, node := range nodes {
		id := fmt.Sprintf("topology_node:%d", node.ID)
		nodeIDs[id] = struct{}{}
		graph.Nodes = append(graph.Nodes, shared.TopologyGraphNode{ID: id, Kind: shared.TopologyNodeKind(node.Kind), Label: node.Name, Source: shared.TopologySourceManual, ManualID: node.ID, Note: node.Note, Position: positions[id]})
	}
	links, err := rt.listLinks(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, link := range links {
		from := link.FromKind + ":" + link.FromID
		to := link.ToKind + ":" + link.ToID
		if _, ok := nodeIDs[from]; !ok {
			continue
		}
		if _, ok := nodeIDs[to]; !ok {
			continue
		}
		graph.Links = append(graph.Links, shared.TopologyGraphLink{ID: fmt.Sprintf("manual:%d", link.ID), From: from, To: to, Label: link.Label, Source: shared.TopologySourceManual, ManualID: link.ID})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(graph)
}

func (rt runtime) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := rt.db.ExecContext(r.Context(), `insert into topology_nodes (name, kind, note, created_at, updated_at) values (?, ?, ?, ?, ?)`, strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("kind")), strings.TrimSpace(r.FormValue("note")), now, now); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (rt runtime) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := rt.db.ExecContext(r.Context(), `update topology_nodes set name = ?, kind = ?, note = ?, updated_at = ? where id = ?`, name, strings.TrimSpace(r.FormValue("kind")), strings.TrimSpace(r.FormValue("note")), now, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (rt runtime) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := rt.db.ExecContext(r.Context(), `delete from topology_links where (from_kind = 'topology_node' and from_id = ?) or (to_kind = 'topology_node' and to_id = ?)`, strconv.FormatInt(id, 10), strconv.FormatInt(id, 10)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := rt.db.ExecContext(r.Context(), `delete from topology_node_positions where subject_kind = 'topology_node' and subject_id = ?`, strconv.FormatInt(id, 10)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := rt.db.ExecContext(r.Context(), `delete from topology_nodes where id = ?`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (rt runtime) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fromKind, fromID, err := parseEndpoint(r.FormValue("from"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	toKind, toID, err := parseEndpoint(r.FormValue("to"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := rt.db.ExecContext(r.Context(), `insert into topology_links (from_kind, from_id, to_kind, to_id, label, source, created_at, updated_at) values (?, ?, ?, ?, ?, 'manual', ?, ?)`, fromKind, fromID, toKind, toID, strings.TrimSpace(r.FormValue("label")), now, now); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (rt runtime) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := rt.db.ExecContext(r.Context(), `delete from topology_links where id = ?`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (rt runtime) handleUpdateLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := rt.db.ExecContext(r.Context(), `update topology_links set label = ?, updated_at = ? where id = ? and source = 'manual'`, strings.TrimSpace(r.FormValue("label")), now, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (rt runtime) handleSaveLayout(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Positions []positionUpdate `json:"positions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range payload.Positions {
		kind, id, err := parseEndpoint(item.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if kind != "target" && kind != "topology_node" {
			http.Error(w, "unsupported topology layout subject", http.StatusBadRequest)
			return
		}
		if _, err := rt.db.ExecContext(r.Context(), `
			insert into topology_node_positions (subject_kind, subject_id, x, y, updated_at)
			values (?, ?, ?, ?, ?)
			on conflict(subject_kind, subject_id) do update set
				x = excluded.x,
				y = excluded.y,
				updated_at = excluded.updated_at`,
			kind, id, item.X, item.Y, now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (rt runtime) handleResetLayout(w http.ResponseWriter, r *http.Request) {
	if _, err := rt.db.ExecContext(r.Context(), `delete from topology_node_positions`); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (rt runtime) notFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func (rt runtime) listNodes(r *http.Request) ([]manualNode, error) {
	rows, err := rt.db.QueryContext(r.Context(), `select id, name, kind, note from topology_nodes order by name collate nocase`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []manualNode
	for rows.Next() {
		var node manualNode
		if err := rows.Scan(&node.ID, &node.Name, &node.Kind, &node.Note); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (rt runtime) listLinks(r *http.Request) ([]manualLink, error) {
	rows, err := rt.db.QueryContext(r.Context(), `select id, from_kind, from_id, to_kind, to_id, label from topology_links order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []manualLink
	for rows.Next() {
		var link manualLink
		if err := rows.Scan(&link.ID, &link.FromKind, &link.FromID, &link.ToKind, &link.ToID, &link.Label); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func (rt runtime) listPositions(r *http.Request) (map[string]*shared.TopologyPosition, error) {
	rows, err := rt.db.QueryContext(r.Context(), `select subject_kind, subject_id, x, y from topology_node_positions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*shared.TopologyPosition{}
	for rows.Next() {
		var kind, id string
		var pos shared.TopologyPosition
		if err := rows.Scan(&kind, &id, &pos.X, &pos.Y); err != nil {
			return nil, err
		}
		out[kind+":"+id] = &pos
	}
	return out, rows.Err()
}

func parseEndpoint(raw string) (string, string, error) {
	kind, id, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok || kind == "" || id == "" {
		return "", "", sql.ErrNoRows
	}
	return kind, id, nil
}
