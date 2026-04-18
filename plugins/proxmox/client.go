package proxmox

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type ProxmoxClient struct {
	baseURL     string
	tokenID     string
	tokenSecret string
	httpClient  *http.Client
}

func NewProxmoxClient(apiURL, tokenID, tokenSecret string, tlsInsecure bool) (*ProxmoxClient, error) {
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		return nil, fmt.Errorf("api_url is required")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("api_url must include host")
	}
	if parsed.Port() == "" {
		parsed.Host = parsed.Host + ":8006"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsInsecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // User opt-in for local Proxmox certificates.
	}
	return &ProxmoxClient{
		baseURL:     strings.TrimRight(parsed.String(), "/"),
		tokenID:     strings.TrimSpace(tokenID),
		tokenSecret: strings.TrimSpace(tokenSecret),
		httpClient:  &http.Client{Timeout: 20 * time.Second, Transport: transport},
	}, nil
}

func (c *ProxmoxClient) ListVMs(ctx context.Context, node string) ([]guest, error) {
	var raw []proxmoxGuestResponse
	if err := c.get(ctx, "/api2/json/nodes/"+url.PathEscape(node)+"/qemu", &raw); err != nil {
		return nil, err
	}
	guests := make([]guest, 0, len(raw))
	for _, item := range raw {
		guests = append(guests, item.guest("qemu", node))
	}
	return guests, nil
}

func (c *ProxmoxClient) ListLXCs(ctx context.Context, node string) ([]guest, error) {
	var raw []proxmoxGuestResponse
	if err := c.get(ctx, "/api2/json/nodes/"+url.PathEscape(node)+"/lxc", &raw); err != nil {
		return nil, err
	}
	guests := make([]guest, 0, len(raw))
	for _, item := range raw {
		guests = append(guests, item.guest("lxc", node))
	}
	return guests, nil
}

func (c *ProxmoxClient) VMStatus(ctx context.Context, node string, vmid int) (guestStatus, error) {
	return c.guestStatus(ctx, node, "qemu", vmid)
}

func (c *ProxmoxClient) LXCStatus(ctx context.Context, node string, vmid int) (guestStatus, error) {
	return c.guestStatus(ctx, node, "lxc", vmid)
}

func (c *ProxmoxClient) StartVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.action(ctx, node, "qemu", vmid, "start")
}

func (c *ProxmoxClient) StopVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.action(ctx, node, "qemu", vmid, "stop")
}

func (c *ProxmoxClient) RebootVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.action(ctx, node, "qemu", vmid, "reboot")
}

func (c *ProxmoxClient) StartLXC(ctx context.Context, node string, vmid int) (string, error) {
	return c.action(ctx, node, "lxc", vmid, "start")
}

func (c *ProxmoxClient) StopLXC(ctx context.Context, node string, vmid int) (string, error) {
	return c.action(ctx, node, "lxc", vmid, "stop")
}

func (c *ProxmoxClient) RebootLXC(ctx context.Context, node string, vmid int) (string, error) {
	return c.action(ctx, node, "lxc", vmid, "reboot")
}

func (c *ProxmoxClient) NodeStatus(ctx context.Context, node string) (nodeStatus, error) {
	var raw proxmoxNodeStatusResponse
	if err := c.get(ctx, "/api2/json/nodes/"+url.PathEscape(node)+"/status", &raw); err != nil {
		return nodeStatus{}, err
	}
	return nodeStatus{
		Node:        node,
		CPU:         raw.CPU,
		MemoryUsed:  raw.Memory.Used,
		MemoryTotal: raw.Memory.Total,
		DiskUsed:    raw.RootFS.Used,
		DiskTotal:   raw.RootFS.Total,
		Uptime:      raw.Uptime,
		LoadAverage: raw.LoadAverage,
	}, nil
}

func (c *ProxmoxClient) ClusterResources(ctx context.Context) ([]clusterResource, error) {
	var raw []proxmoxClusterResourceResponse
	if err := c.get(ctx, "/api2/json/cluster/resources", &raw); err != nil {
		return nil, err
	}
	out := make([]clusterResource, 0, len(raw))
	for _, item := range raw {
		out = append(out, clusterResource{
			Type:      item.Type,
			ID:        item.ID,
			Node:      item.Node,
			Name:      item.Name,
			Status:    item.Status,
			CPU:       item.CPU,
			Memory:    item.Memory,
			MaxMemory: item.MaxMemory,
			Disk:      item.Disk,
			MaxDisk:   item.MaxDisk,
			Uptime:    item.Uptime,
			VMID:      item.VMID,
		})
	}
	return out, nil
}

func (c *ProxmoxClient) guestStatus(ctx context.Context, node, guestType string, vmid int) (guestStatus, error) {
	var raw map[string]any
	if err := c.get(ctx, fmt.Sprintf("/api2/json/nodes/%s/%s/%d/status/current", url.PathEscape(node), guestType, vmid), &raw); err != nil {
		return guestStatus{}, err
	}
	status := guestStatus{
		guest: guest{
			VMID:      vmid,
			Type:      guestType,
			Node:      node,
			Name:      stringValue(raw["name"]),
			Status:    stringValue(raw["status"]),
			CPU:       floatValue(raw["cpu"]),
			Memory:    uintValue(raw["mem"]),
			MaxMemory: uintValue(raw["maxmem"]),
			DiskUsed:  uintValue(raw["disk"]),
			DiskTotal: uintValue(raw["maxdisk"]),
			Uptime:    intValue(raw["uptime"]),
		},
		PID:        int(intValue(raw["pid"])),
		QMPStatus:  stringValue(raw["qmpstatus"]),
		Lock:       stringValue(raw["lock"]),
		HAState:    stringValue(raw["hastate"]),
		Template:   boolValue(raw["template"]),
		RawCurrent: raw,
	}
	return status, nil
}

func (c *ProxmoxClient) action(ctx context.Context, node, guestType string, vmid int, action string) (string, error) {
	var upid string
	err := c.post(ctx, fmt.Sprintf("/api2/json/nodes/%s/%s/%d/status/%s", url.PathEscape(node), guestType, vmid, action), nil, &upid)
	return upid, err
}

func (c *ProxmoxClient) get(ctx context.Context, endpoint string, out any) error {
	return c.do(ctx, http.MethodGet, endpoint, nil, out)
}

func (c *ProxmoxClient) post(ctx context.Context, endpoint string, body io.Reader, out any) error {
	return c.do(ctx, http.MethodPost, endpoint, body, out)
}

func (c *ProxmoxClient) do(ctx context.Context, method, endpoint string, body io.Reader, out any) error {
	reqURL := c.baseURL + "/" + strings.TrimLeft(path.Clean("/"+endpoint), "/")
	if body == nil && method == http.MethodPost {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxmox request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("proxmox API returned %s: %s", resp.Status, msg)
	}
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(wrapper.Data, out)
}

type proxmoxGuestResponse struct {
	VMID      int     `json:"vmid"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CPU       float64 `json:"cpu"`
	Memory    uint64  `json:"mem"`
	MaxMemory uint64  `json:"maxmem"`
	DiskUsed  uint64  `json:"disk"`
	DiskTotal uint64  `json:"maxdisk"`
	Uptime    int64   `json:"uptime"`
}

func (r proxmoxGuestResponse) guest(guestType, node string) guest {
	return guest{
		VMID:      r.VMID,
		Name:      r.Name,
		Type:      guestType,
		Status:    r.Status,
		CPU:       r.CPU,
		Memory:    r.Memory,
		MaxMemory: r.MaxMemory,
		DiskUsed:  r.DiskUsed,
		DiskTotal: r.DiskTotal,
		Uptime:    r.Uptime,
		Node:      node,
	}
}

type proxmoxNodeStatusResponse struct {
	CPU         float64  `json:"cpu"`
	Uptime      int64    `json:"uptime"`
	LoadAverage []string `json:"loadavg"`
	Memory      struct {
		Used  uint64 `json:"used"`
		Total uint64 `json:"total"`
	} `json:"memory"`
	RootFS struct {
		Used  uint64 `json:"used"`
		Total uint64 `json:"total"`
	} `json:"rootfs"`
}

type proxmoxClusterResourceResponse struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Node      string  `json:"node"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CPU       float64 `json:"cpu"`
	Memory    uint64  `json:"mem"`
	MaxMemory uint64  `json:"maxmem"`
	Disk      uint64  `json:"disk"`
	MaxDisk   uint64  `json:"maxdisk"`
	Uptime    int64   `json:"uptime"`
	VMID      int     `json:"vmid"`
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return ""
	}
}

func floatValue(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case json.Number:
		f, _ := value.Float64()
		return f
	default:
		return 0
	}
}

func uintValue(v any) uint64 {
	switch value := v.(type) {
	case float64:
		return uint64(value)
	case int:
		return uint64(value)
	case json.Number:
		n, _ := strconv.ParseUint(value.String(), 10, 64)
		return n
	default:
		return 0
	}
}

func intValue(v any) int64 {
	switch value := v.(type) {
	case float64:
		return int64(value)
	case int:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
}

func boolValue(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case float64:
		return value != 0
	case string:
		return value == "1" || strings.EqualFold(value, "true")
	default:
		return false
	}
}
