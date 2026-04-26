package proxmox

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"insylus/internal/httpx"
	"insylus/internal/pluginhost"
)

type proxmoxPageData struct {
	Nodes   []tokenSummary
	Devices []pluginhost.InventoryDevice
}

type proxmoxTokenRequest struct {
	DeviceID    string `json:"device_id"`
	Node        string `json:"node"`
	NodeName    string `json:"node_name"`
	APIURL      string `json:"api_url"`
	TokenID     string `json:"token_id"`
	TokenSecret string `json:"token_secret"`
	Role        string `json:"role"`
	TLSInsecure bool   `json:"tls_insecure"`
}

type runtime struct {
	store  store
	render func(http.ResponseWriter, string, any)
}

func (rt runtime) handleProxmoxPage(w http.ResponseWriter, r *http.Request) {
	nodes, err := rt.store.listNodes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	devices, err := rt.store.inventory.ListDevices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "proxmox.html", proxmoxPageData{Nodes: nodes, Devices: devices})
}

func (rt runtime) handleProxmoxNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := rt.store.listNodes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (rt runtime) handleProxmoxSetToken(w http.ResponseWriter, r *http.Request) {
	req, ok := rt.proxmoxTokenRequest(w, r)
	if !ok {
		return
	}
	record, existing, err := rt.resolveProxmoxConfigTarget(r, req)
	if err != nil {
		rt.writeProxmoxResolveError(w, r, req.Node, err)
		return
	}
	nodeName := firstNonEmpty(req.NodeName, existing.NodeName, defaultNodeName(record))
	token, err := rt.store.setToken(r.Context(), token{
		DeviceID:    record.ID,
		NodeName:    nodeName,
		APIURL:      strings.TrimRight(strings.TrimSpace(req.APIURL), "/"),
		TokenID:     req.TokenID,
		TokenSecret: req.TokenSecret,
		Role:        req.Role,
		TLSInsecure: req.TLSInsecure,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/proxmox", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (rt runtime) handleProxmoxDeleteToken(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/proxmox", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (rt runtime) handleProxmoxGuests(w http.ResponseWriter, r *http.Request) {
	list, ok := rt.proxmoxGuestList(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (rt runtime) handleProxmoxVMs(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.proxmoxClientForRequest(w, r)
	if !ok {
		return
	}
	vms, err := client.ListVMs(r.Context(), token.NodeName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, vms)
}

func (rt runtime) handleProxmoxLXCs(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.proxmoxClientForRequest(w, r)
	if !ok {
		return
	}
	lxcs, err := client.ListLXCs(r.Context(), token.NodeName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, lxcs)
}

func (rt runtime) handleProxmoxGuestStatus(w http.ResponseWriter, r *http.Request) {
	target, ok := proxmoxTargetFromRequest(w, r)
	if !ok {
		return
	}
	_, token, client, ok := rt.proxmoxClientForRequest(w, r)
	if !ok {
		return
	}
	guest, err := rt.resolveProxmoxGuest(r, client, token.NodeName, target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	status, err := rt.proxmoxGuestStatus(r, client, token.NodeName, guest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (rt runtime) handleProxmoxStart(w http.ResponseWriter, r *http.Request) {
	rt.handleProxmoxAction(w, r, "start")
}

func (rt runtime) handleProxmoxStop(w http.ResponseWriter, r *http.Request) {
	rt.handleProxmoxAction(w, r, "stop")
}

func (rt runtime) handleProxmoxRestart(w http.ResponseWriter, r *http.Request) {
	rt.handleProxmoxAction(w, r, "restart")
}

func (rt runtime) handleProxmoxAction(w http.ResponseWriter, r *http.Request, action string) {
	target, ok := proxmoxTargetFromRequest(w, r)
	if !ok {
		return
	}
	_, token, client, ok := rt.proxmoxClientForRequest(w, r)
	if !ok {
		return
	}
	guest, err := rt.resolveProxmoxGuest(r, client, token.NodeName, target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var upid string
	switch action {
	case "start":
		upid, err = startProxmoxGuest(r, client, token.NodeName, guest)
	case "stop":
		upid, err = stopProxmoxGuest(r, client, token.NodeName, guest)
	case "restart":
		upid, err = rebootProxmoxGuest(r, client, token.NodeName, guest)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, actionResult{
		Node:   token.NodeName,
		VMID:   guest.VMID,
		Type:   guest.Type,
		Action: action,
		UPID:   upid,
		Status: "submitted",
	})
}

func (rt runtime) handleProxmoxNodeStatus(w http.ResponseWriter, r *http.Request) {
	_, token, client, ok := rt.proxmoxClientForRequest(w, r)
	if !ok {
		return
	}
	status, err := client.NodeStatus(r.Context(), token.NodeName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (rt runtime) handleProxmoxClusterStatus(w http.ResponseWriter, r *http.Request) {
	_, _, client, ok := rt.proxmoxClientForRequest(w, r)
	if !ok {
		return
	}
	resources, err := client.ClusterResources(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, resources)
}

func (rt runtime) proxmoxTokenRequest(w http.ResponseWriter, r *http.Request) (proxmoxTokenRequest, bool) {
	var req proxmoxTokenRequest
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
	req.Node = strings.TrimSpace(r.FormValue("node"))
	req.NodeName = strings.TrimSpace(r.FormValue("node_name"))
	req.APIURL = strings.TrimSpace(r.FormValue("api_url"))
	req.TokenID = strings.TrimSpace(r.FormValue("token_id"))
	req.TokenSecret = strings.TrimSpace(r.FormValue("token_secret"))
	req.Role = strings.TrimSpace(r.FormValue("role"))
	req.TLSInsecure = r.FormValue("tls_insecure") == "on" || r.FormValue("tls_insecure") == "true"
	return req, true
}

func (rt runtime) resolveProxmoxConfigTarget(r *http.Request, req proxmoxTokenRequest) (pluginhost.InventoryDevice, tokenSummary, error) {
	if strings.TrimSpace(req.DeviceID) != "" {
		record, err := rt.store.inventory.GetDevice(r.Context(), req.DeviceID)
		if err != nil {
			return pluginhost.InventoryDevice{}, tokenSummary{}, err
		}
		summary, err := rt.store.getTokenSummary(r.Context(), req.DeviceID)
		if errors.Is(err, sql.ErrNoRows) {
			summary = tokenSummary{
				DeviceID:   record.ID,
				DeviceName: record.Name,
				Hostname:   record.Hostname,
				NodeName:   defaultNodeName(record),
			}
		} else if err != nil {
			return pluginhost.InventoryDevice{}, tokenSummary{}, err
		}
		return record, summary, nil
	}
	if strings.TrimSpace(req.Node) != "" {
		record, summary, err := rt.store.resolveDeviceForConfig(r.Context(), req.Node)
		if err == nil {
			return record, summary, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return pluginhost.InventoryDevice{}, tokenSummary{}, err
		}
	}
	name := firstNonEmpty(req.NodeName, req.Node, req.APIURL)
	if name == "" {
		return pluginhost.InventoryDevice{}, tokenSummary{}, fmt.Errorf("node, node_name, api_url, or device_id is required")
	}
	target, err := rt.store.targets.Create(r.Context(), pluginhost.TargetInput{
		Name:      name,
		Kind:      "proxmox-node",
		Hostname:  req.Node,
		APIURL:    req.APIURL,
		CreatedBy: "proxmox",
	})
	if err != nil {
		return pluginhost.InventoryDevice{}, tokenSummary{}, err
	}
	record := pluginhost.InventoryDevice{ID: target.ID, Name: target.Name, Hostname: target.Hostname, IPs: target.IPs, DeviceType: target.Kind, Purpose: target.Kind}
	return record, tokenSummary{
		DeviceID:   record.ID,
		DeviceName: record.Name,
		Hostname:   record.Hostname,
		NodeName:   defaultNodeName(record),
	}, nil
}

func (rt runtime) writeProxmoxResolveError(w http.ResponseWriter, r *http.Request, query string, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, fmt.Sprintf("no device found for Proxmox node %q", query), http.StatusNotFound)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func (rt runtime) proxmoxClientForRequest(w http.ResponseWriter, r *http.Request) (pluginhost.InventoryDevice, token, *ProxmoxClient, bool) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	record, err := rt.store.inventory.GetDevice(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return pluginhost.InventoryDevice{}, token{}, nil, false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return pluginhost.InventoryDevice{}, token{}, nil, false
	}
	authToken, err := rt.store.getToken(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "no Proxmox API token configured for "+record.Name, http.StatusBadRequest)
			return pluginhost.InventoryDevice{}, token{}, nil, false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return pluginhost.InventoryDevice{}, token{}, nil, false
	}
	apiURL := authToken.APIURL
	if apiURL == "" {
		apiURL = defaultProxmoxAPIURL(record)
	}
	client, err := NewProxmoxClient(apiURL, authToken.TokenID, authToken.TokenSecret, authToken.TLSInsecure)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return pluginhost.InventoryDevice{}, token{}, nil, false
	}
	return record, authToken, client, true
}

func (rt runtime) proxmoxGuestList(w http.ResponseWriter, r *http.Request) (guestList, bool) {
	_, token, client, ok := rt.proxmoxClientForRequest(w, r)
	if !ok {
		return guestList{}, false
	}
	vms, err := client.ListVMs(r.Context(), token.NodeName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return guestList{}, false
	}
	lxcs, err := client.ListLXCs(r.Context(), token.NodeName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return guestList{}, false
	}
	guests := append(append([]guest{}, vms...), lxcs...)
	sort.SliceStable(guests, func(i, j int) bool {
		if guests[i].VMID != guests[j].VMID {
			return guests[i].VMID < guests[j].VMID
		}
		return guests[i].Type < guests[j].Type
	})
	return guestList{
		Node:   summarizeToken(token),
		VMs:    vms,
		LXCs:   lxcs,
		Guests: guests,
	}, true
}

func (rt runtime) resolveProxmoxGuest(r *http.Request, client *ProxmoxClient, node, target string) (guest, error) {
	vms, err := client.ListVMs(r.Context(), node)
	if err != nil {
		return guest{}, err
	}
	lxcs, err := client.ListLXCs(r.Context(), node)
	if err != nil {
		return guest{}, err
	}
	guests := append(vms, lxcs...)
	var matches []guest
	for _, guest := range guests {
		if strconv.Itoa(guest.VMID) == target || strings.EqualFold(guest.Name, target) {
			matches = append(matches, guest)
		}
	}
	if len(matches) == 0 {
		return guest{}, fmt.Errorf("no VM or LXC named %q found on %s", target, node)
	}
	if len(matches) > 1 {
		return guest{}, fmt.Errorf("multiple guests match %q on %s; use vmid", target, node)
	}
	return matches[0], nil
}

func (rt runtime) proxmoxGuestStatus(r *http.Request, client *ProxmoxClient, node string, guest guest) (guestStatus, error) {
	if guest.Type == "lxc" {
		return client.LXCStatus(r.Context(), node, guest.VMID)
	}
	return client.VMStatus(r.Context(), node, guest.VMID)
}

func startProxmoxGuest(r *http.Request, client *ProxmoxClient, node string, guest guest) (string, error) {
	if guest.Type == "lxc" {
		return client.StartLXC(r.Context(), node, guest.VMID)
	}
	return client.StartVM(r.Context(), node, guest.VMID)
}

func stopProxmoxGuest(r *http.Request, client *ProxmoxClient, node string, guest guest) (string, error) {
	if guest.Type == "lxc" {
		return client.StopLXC(r.Context(), node, guest.VMID)
	}
	return client.StopVM(r.Context(), node, guest.VMID)
}

func rebootProxmoxGuest(r *http.Request, client *ProxmoxClient, node string, guest guest) (string, error) {
	if guest.Type == "lxc" {
		return client.RebootLXC(r.Context(), node, guest.VMID)
	}
	return client.RebootVM(r.Context(), node, guest.VMID)
}

func proxmoxTargetFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	target := strings.TrimSpace(r.PathValue("target"))
	if target == "" {
		target = strings.TrimSpace(r.PathValue("vmid"))
	}
	if target == "" {
		target = strings.TrimSpace(r.URL.Query().Get("target"))
	}
	if target == "" {
		http.Error(w, "guest target is required", http.StatusBadRequest)
		return "", false
	}
	return target, true
}

func defaultProxmoxAPIURL(record pluginhost.InventoryDevice) string {
	host := ""
	if len(record.IPs) > 0 {
		host = record.IPs[0]
	} else if strings.TrimSpace(record.Hostname) != "" {
		host = record.Hostname
	} else {
		host = record.Name
	}
	return "https://" + host + ":8006"
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html") ||
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return httpx.DecodeJSON(w, r, dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSON(w, status, v)
}
