package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"insylus/internal/shared"
)

type topologyData struct {
	Devices []DeviceRecord
	Nodes   []ManualTopologyNode
	Links   []ManualTopologyLink
	Options []topologyEndpointOption
	Error   string
}

type topologyEndpointOption struct {
	Value string
	Label string
}

func (a *App) handleTopologyPage(w http.ResponseWriter, r *http.Request) {
	data, err := a.topologyPageData(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "topology.html", data)
}

func (a *App) handleTopologyGraph(w http.ResponseWriter, r *http.Request) {
	graph, err := a.buildTopologyGraph(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, http.StatusOK, graph)
}

func (a *App) handleCreateTopologyNode(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err := a.store.CreateTopologyNode(
		r.Context(),
		r.FormValue("name"),
		shared.TopologyNodeKind(strings.TrimSpace(r.FormValue("kind"))),
		r.FormValue("note"),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (a *App) handleCreateTopologyLink(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fromKind, fromID, err := parseTopologyEndpoint(r.FormValue("from"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	toKind, toID, err := parseTopologyEndpoint(r.FormValue("to"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := a.store.CreateTopologyLink(r.Context(), fromKind, fromID, toKind, toID, r.FormValue("label")); err != nil {
		if errors.Is(err, ErrDuplicateTopologyLink) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (a *App) handleDeleteTopologyLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteTopologyLink(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (a *App) handleDeleteTopologyNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteTopologyNode(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/topology", http.StatusSeeOther)
}

func (a *App) topologyPageData(ctx context.Context) (topologyData, error) {
	records, err := a.store.ListDevices(ctx)
	if err != nil {
		return topologyData{}, err
	}
	nodes, err := a.store.ListTopologyNodes(ctx)
	if err != nil {
		return topologyData{}, err
	}
	links, err := a.store.ListTopologyLinks(ctx)
	if err != nil {
		return topologyData{}, err
	}
	options := make([]topologyEndpointOption, 0, len(records)+len(nodes))
	for _, record := range records {
		options = append(options, topologyEndpointOption{
			Value: "device:" + record.Device.ID,
			Label: "Device: " + record.Device.Name,
		})
	}
	for _, node := range nodes {
		options = append(options, topologyEndpointOption{
			Value: fmt.Sprintf("topology_node:%d", node.ID),
			Label: fmt.Sprintf("%s: %s", node.Kind, node.Name),
		})
	}
	return topologyData{Devices: records, Nodes: nodes, Links: links, Options: options}, nil
}

func (a *App) buildTopologyGraph(ctx context.Context) (shared.TopologyGraph, error) {
	records, err := a.store.ListDevices(ctx)
	if err != nil {
		return shared.TopologyGraph{}, err
	}
	nodes, err := a.store.ListTopologyNodes(ctx)
	if err != nil {
		return shared.TopologyGraph{}, err
	}
	links, err := a.store.ListTopologyLinks(ctx)
	if err != nil {
		return shared.TopologyGraph{}, err
	}
	return topologyGraphFromRecords(records, nodes, links), nil
}

// topologyCluster holds a named cluster with its member node IDs.
type topologyCluster struct {
	ID    string
	Label string
	Kind  shared.TopologyNodeKind
	Nodes []string // node IDs that belong to this cluster
}

func topologyGraphFromRecords(records []DeviceRecord, manualNodes []ManualTopologyNode, manualLinks []ManualTopologyLink) shared.TopologyGraph {
	graph := shared.TopologyGraph{}
	nodeIDs := make(map[string]struct{})
	addNode := func(node shared.TopologyGraphNode) {
		if _, ok := nodeIDs[node.ID]; ok {
			return
		}
		nodeIDs[node.ID] = struct{}{}
		graph.Nodes = append(graph.Nodes, node)
	}
	addLink := func(link shared.TopologyGraphLink) {
		if _, ok := nodeIDs[link.From]; !ok {
			return
		}
		if _, ok := nodeIDs[link.To]; !ok {
			return
		}
		graph.Links = append(graph.Links, link)
	}

	// ── Build cluster map ──────────────────────────────────────────────────────
	// parentID → cluster for devices that have children
	clusterByParent := make(map[string]*topologyCluster)
	// nodeID → clusterID
	clusterByNode := make(map[string]string)

	// First pass: create clusters for devices that are parents
	for _, record := range records {
		if record.Resolved.ParentState == shared.ParentStateLinked && record.Resolved.ParentDeviceID == "" {
			continue // only create clusters for devices that have children
		}
		// Does this device have any children?
		hasChildren := false
		for _, other := range records {
			if other.Device.ID == record.Device.ID {
				continue
			}
			if other.Resolved.ParentState == shared.ParentStateLinked &&
				other.Resolved.ParentDeviceID == record.Device.ID {
				hasChildren = true
				break
			}
		}
		if hasChildren {
			clusterByParent[record.Device.ID] = &topologyCluster{
				ID:    "cluster:device:" + record.Device.ID,
				Label: record.Device.Name,
				Kind:  shared.TopologyNodeKindDevice,
				Nodes: []string{"device:" + record.Device.ID},
			}
		}
	}
	// Second pass: assign every device to a cluster
	for _, record := range records {
		deviceNodeID := "device:" + record.Device.ID
		if record.Resolved.ParentState == shared.ParentStateLinked && record.Resolved.ParentDeviceID != "" {
			clusterByNode[deviceNodeID] = "cluster:device:" + record.Resolved.ParentDeviceID
			if c, ok := clusterByParent[record.Resolved.ParentDeviceID]; ok {
				c.Nodes = append(c.Nodes, deviceNodeID)
			}
		}
	}

	// Collect standalone devices (no parent, no children) into a root bucket for internal layout hints.
	rootClusterID := "cluster:root"
	for _, record := range records {
		deviceNodeID := "device:" + record.Device.ID
		if _, belongs := clusterByNode[deviceNodeID]; !belongs {
			if c, ok := clusterByParent[record.Device.ID]; ok {
				clusterByNode[deviceNodeID] = c.ID // standalone parent still belongs to its own cluster
			} else {
				clusterByNode[deviceNodeID] = rootClusterID
			}
		}
	}

	// Add manual topology nodes as their own clusters
	for _, node := range manualNodes {
		clusterID := fmt.Sprintf("cluster:topology_node:%d", node.ID)
		nodeID := fmt.Sprintf("topology_node:%d", node.ID)
		clusterByNode[nodeID] = clusterID
	}

	manualPairKeys := make(map[string]struct{}, len(manualLinks))
	for _, link := range manualLinks {
		manualPairKeys[linkKey(endpointGraphID(link.FromKind, link.FromID), endpointGraphID(link.ToKind, link.ToID), link.Label)] = struct{}{}
	}

	for _, node := range manualNodes {
		nodeID := fmt.Sprintf("topology_node:%d", node.ID)
		addNode(shared.TopologyGraphNode{
			ID:        nodeID,
			Kind:      node.Kind,
			Label:     node.Name,
			Source:    shared.TopologySourceManual,
			Note:      node.Note,
			ManualID:  node.ID,
			ClusterID: fmt.Sprintf("cluster:topology_node:%d", node.ID),
		})
	}
	for _, record := range records {
		parentGroup := ""
		if record.Resolved.ParentState == shared.ParentStateUnknown {
			parentGroup = "unknown"
		} else if record.Resolved.ParentState == shared.ParentStateNone {
			parentGroup = "none"
		}
		deviceID := "device:" + record.Device.ID
		addNode(shared.TopologyGraphNode{
			ID:          deviceID,
			Kind:        shared.TopologyNodeKindDevice,
			Label:       record.Device.Name,
			DeviceID:    record.Device.ID,
			DeviceType:  record.Resolved.EffectiveDeviceType,
			Purpose:     record.Resolved.Purpose,
			Source:      shared.TopologySourceDiscovered,
			URL:         "/devices/" + record.Device.ID,
			ParentGroup: parentGroup,
			ClusterID:   clusterByNode[deviceID],
			Status: &shared.TopologyNodeStatus{
				LastSeenAt:   record.Device.LastSeenAt,
				AgentVersion: record.Device.AgentVersion,
			},
		})
	}

	for _, record := range records {
		deviceID := "device:" + record.Device.ID
		if record.Resolved.ParentState == shared.ParentStateLinked && record.Resolved.ParentDeviceID != "" {
			from := "device:" + record.Resolved.ParentDeviceID
			key := linkKey(from, deviceID, "")
			if _, manual := manualPairKeys[key]; !manual {
				source := record.Resolved.ParentSource
				if source == "" {
					source = shared.TopologySourceInferred
				}
				addLink(shared.TopologyGraphLink{
					ID:     "parent:" + record.Device.ID,
					From:   from,
					To:     deviceID,
					Label:  "parent",
					Source: source,
				})
			}
		}
		for i, workload := range record.Discovery.Workloads {
			if duplicateEnrolledVirtualWorkload(records, clusterByNode, clusterByNode[deviceID], workload) {
				continue
			}
			workloadID := fmt.Sprintf("workload:%s:%d", record.Device.ID, i)
			clusterByNode[workloadID] = clusterByNode[deviceID]
			if c, ok := clusterByParent[record.Device.ID]; ok {
				c.Nodes = append(c.Nodes, workloadID)
			}
			addNode(shared.TopologyGraphNode{
				ID:          workloadID,
				Kind:        shared.TopologyNodeKindWorkload,
				Label:       workload.Name,
				Source:      shared.TopologySourceDiscovered,
				ParentGroup: deviceID,
				Note:        string(workload.Kind),
				ClusterID:   clusterByNode[workloadID],
			})
			addLink(shared.TopologyGraphLink{
				ID:     fmt.Sprintf("workload-link:%s:%d", record.Device.ID, i),
				From:   deviceID,
				To:     workloadID,
				Label:  string(workload.Kind),
				Source: shared.TopologySourceDiscovered,
			})
		}
	}
	for _, link := range manualLinks {
		addLink(shared.TopologyGraphLink{
			ID:       fmt.Sprintf("manual:%d", link.ID),
			From:     endpointGraphID(link.FromKind, link.FromID),
			To:       endpointGraphID(link.ToKind, link.ToID),
			Label:    link.Label,
			Source:   shared.TopologySourceManual,
			ManualID: link.ID,
		})
	}
	return graph
}

func duplicateEnrolledVirtualWorkload(records []DeviceRecord, clusterByNode map[string]string, clusterID string, workload shared.Workload) bool {
	if workload.Kind != shared.WorkloadKindVM && workload.Kind != shared.WorkloadKindLXC {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(workload.Name))
	if name == "" {
		return false
	}
	for _, record := range records {
		deviceNodeID := "device:" + record.Device.ID
		if clusterByNode[deviceNodeID] != clusterID {
			continue
		}
		if strings.EqualFold(record.Device.Name, name) || strings.EqualFold(record.Device.Hostname, name) {
			return true
		}
	}
	return false
}

func parseTopologyEndpoint(raw string) (shared.TopologyEndpointKind, string, error) {
	kind, id, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok || strings.TrimSpace(id) == "" {
		return "", "", fmt.Errorf("invalid topology endpoint")
	}
	switch shared.TopologyEndpointKind(kind) {
	case shared.TopologyEndpointDevice:
		return shared.TopologyEndpointDevice, id, nil
	case shared.TopologyEndpointTopologyNode:
		return shared.TopologyEndpointTopologyNode, id, nil
	default:
		return "", "", fmt.Errorf("invalid topology endpoint kind: %s", kind)
	}
}

func endpointGraphID(kind shared.TopologyEndpointKind, id string) string {
	return string(kind) + ":" + id
}

func linkKey(from, to, label string) string {
	return from + "\x00" + to + "\x00" + strings.TrimSpace(label)
}
