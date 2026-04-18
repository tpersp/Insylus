package proxmox

import "time"

type token struct {
	DeviceID     string
	NodeName     string
	APIURL       string
	TokenID      string
	TokenSecret  string
	Role         string
	TLSInsecure  bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeviceName   string
	DeviceHost   string
	DeviceOnline bool
}

type tokenSummary struct {
	DeviceID    string    `json:"device_id"`
	DeviceName  string    `json:"device_name"`
	Hostname    string    `json:"hostname,omitempty"`
	NodeName    string    `json:"node_name"`
	APIURL      string    `json:"api_url,omitempty"`
	TokenID     string    `json:"token_id"`
	Role        string    `json:"role,omitempty"`
	HasToken    bool      `json:"has_token"`
	TLSInsecure bool      `json:"tls_insecure,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type guest struct {
	VMID      int     `json:"vmid"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	CPU       float64 `json:"cpu"`
	Memory    uint64  `json:"memory"`
	MaxMemory uint64  `json:"max_memory"`
	DiskUsed  uint64  `json:"disk_used"`
	DiskTotal uint64  `json:"disk_total"`
	Uptime    int64   `json:"uptime"`
	Node      string  `json:"node,omitempty"`
}

type guestStatus struct {
	guest
	PID        int    `json:"pid,omitempty"`
	QMPStatus  string `json:"qmp_status,omitempty"`
	Lock       string `json:"lock,omitempty"`
	HAState    string `json:"ha_state,omitempty"`
	Template   bool   `json:"template,omitempty"`
	RawCurrent any    `json:"raw_current,omitempty"`
}

type nodeStatus struct {
	Node        string   `json:"node"`
	CPU         float64  `json:"cpu_usage"`
	MemoryUsed  uint64   `json:"memory_used"`
	MemoryTotal uint64   `json:"memory_total"`
	DiskUsed    uint64   `json:"disk_used"`
	DiskTotal   uint64   `json:"disk_total"`
	Uptime      int64    `json:"uptime"`
	LoadAverage []string `json:"load_average,omitempty"`
}

type clusterResource struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Node      string  `json:"node,omitempty"`
	Name      string  `json:"name,omitempty"`
	Status    string  `json:"status,omitempty"`
	CPU       float64 `json:"cpu,omitempty"`
	Memory    uint64  `json:"memory,omitempty"`
	MaxMemory uint64  `json:"max_memory,omitempty"`
	Disk      uint64  `json:"disk,omitempty"`
	MaxDisk   uint64  `json:"max_disk,omitempty"`
	Uptime    int64   `json:"uptime,omitempty"`
	VMID      int     `json:"vmid,omitempty"`
}

type guestList struct {
	Node   tokenSummary `json:"node"`
	VMs    []guest      `json:"vms"`
	LXCs   []guest      `json:"lxcs"`
	Guests []guest      `json:"guests"`
}

type actionResult struct {
	Node    string `json:"node"`
	VMID    int    `json:"vmid"`
	Type    string `json:"type"`
	Action  string `json:"action"`
	UPID    string `json:"upid,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
