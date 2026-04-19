package shared

import (
	"context"
	"time"
)

const (
	AgentCheckInInterval = 15 * time.Second
	DeviceOnlineWindow   = 45 * time.Second
	DefaultManagedUser   = "bob"
)

type AccessMode string

const (
	AccessModeDisabled        AccessMode = "disabled"
	AccessModeAudit           AccessMode = "audit"
	AccessModeDocker          AccessMode = "docker"
	AccessModeSudoPrompted    AccessMode = "sudo_prompted"
	AccessModeSudoPasswordless AccessMode = "sudo_passwordless"
)

type DeviceMode string

const (
	DeviceModeInventoryOnly DeviceMode = "inventory-only"
	DeviceModeAccessManaged DeviceMode = "access-managed"
)

type DeviceType string

const (
	DeviceTypeUnknown     DeviceType = "unknown"
	DeviceTypeBareMetal   DeviceType = "bare-metal"
	DeviceTypeVM          DeviceType = "vm"
	DeviceTypeLXC         DeviceType = "lxc"
	DeviceTypeContainer   DeviceType = "container"
	DeviceTypeProxmoxNode DeviceType = "proxmox-node"
	DeviceTypeDockerHost  DeviceType = "docker-host"
)

type PlatformClass string

const (
	PlatformClassUnknown      PlatformClass = "unknown"
	PlatformClassGenericLinux PlatformClass = "generic-linux"
	PlatformClassRaspberryPi  PlatformClass = "raspberry-pi"
)

type DevicePurpose string

const (
	DevicePurposeUnknown     DevicePurpose = "unknown"
	DevicePurposeDockerHost  DevicePurpose = "docker-host"
	DevicePurposeProxmoxNode DevicePurpose = "proxmox-node"
)

type TopologySource string

const (
	TopologySourceUnknown    TopologySource = "unknown"
	TopologySourceDiscovered TopologySource = "discovered"
	TopologySourceInferred   TopologySource = "inferred"
	TopologySourceManual     TopologySource = "manual"
	TopologySourceNone       TopologySource = "none"
)

type ParentOverrideState string

const (
	ParentOverrideInherit       ParentOverrideState = "inherit"
	ParentOverrideManualDevice  ParentOverrideState = "manual_device"
	ParentOverrideManualUnknown ParentOverrideState = "manual_unknown"
	ParentOverrideManualNone    ParentOverrideState = "manual_none"
)

type ParentState string

const (
	ParentStateLinked  ParentState = "linked"
	ParentStateUnknown ParentState = "unknown"
	ParentStateNone    ParentState = "none"
)

type AgentAutoUpdateOverride string

const (
	AgentAutoUpdateInherit  AgentAutoUpdateOverride = "inherit"
	AgentAutoUpdateEnabled  AgentAutoUpdateOverride = "enabled"
	AgentAutoUpdateDisabled AgentAutoUpdateOverride = "disabled"
)

type AgentUpdateStatus string

const (
	AgentUpdateStatusIdle        AgentUpdateStatus = "idle"
	AgentUpdateStatusAvailable   AgentUpdateStatus = "available"
	AgentUpdateStatusUpdating    AgentUpdateStatus = "updating"
	AgentUpdateStatusUpdated     AgentUpdateStatus = "updated"
	AgentUpdateStatusFailed      AgentUpdateStatus = "failed"
	AgentUpdateStatusUnsupported AgentUpdateStatus = "unsupported"
)

type WorkloadKind string

const (
	WorkloadKindService   WorkloadKind = "service"
	WorkloadKindContainer WorkloadKind = "container"
	WorkloadKindVM        WorkloadKind = "vm"
	WorkloadKindLXC       WorkloadKind = "lxc"
)

type ServiceHealth string

const (
	ServiceHealthHealthy   ServiceHealth = "healthy"
	ServiceHealthUnhealthy ServiceHealth = "unhealthy"
	ServiceHealthMissing   ServiceHealth = "missing"
	ServiceHealthUnknown   ServiceHealth = "unknown"
)

type TopologyNodeKind string

const (
	TopologyNodeKindInternet    TopologyNodeKind = "internet"
	TopologyNodeKindRouter      TopologyNodeKind = "router"
	TopologyNodeKindSwitch      TopologyNodeKind = "switch"
	TopologyNodeKindAccessPoint TopologyNodeKind = "access-point"
	TopologyNodeKindPatchPanel  TopologyNodeKind = "patch-panel"
	TopologyNodeKindOther       TopologyNodeKind = "other"
	TopologyNodeKindDevice      TopologyNodeKind = "device"
	TopologyNodeKindWorkload    TopologyNodeKind = "workload"
	TopologyNodeKindGroup       TopologyNodeKind = "group"
)

type TopologyEndpointKind string

const (
	TopologyEndpointDevice       TopologyEndpointKind = "device"
	TopologyEndpointTopologyNode TopologyEndpointKind = "topology_node"
)

type Endpoint struct {
	Host string `json:"host,omitempty"`
	Port string `json:"port,omitempty"`
}

type Workload struct {
	Name      string       `json:"name"`
	Kind      WorkloadKind `json:"kind"`
	Image     string       `json:"image,omitempty"`
	State     string       `json:"state,omitempty"`
	Endpoints []Endpoint   `json:"endpoints,omitempty"`
}

type ChildCandidate struct {
	Name      string       `json:"name"`
	Kind      WorkloadKind `json:"kind"`
	IPs       []string     `json:"ips,omitempty"`
	Endpoints []Endpoint   `json:"endpoints,omitempty"`
}

type TopologyDiscovery struct {
	DeviceType      DeviceType       `json:"device_type"`
	Purpose         DevicePurpose    `json:"purpose"`
	PlatformClass   PlatformClass    `json:"platform_class"`
	WakeOnLAN       WakeOnLANInfo    `json:"wol,omitempty"`
	Workloads       []Workload       `json:"workloads,omitempty"`
	ChildCandidates []ChildCandidate `json:"child_candidates,omitempty"`
	Warnings        []string         `json:"warnings,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type WakeOnLANInfo struct {
	Enabled      bool      `json:"enabled"`
	Supported    bool      `json:"supported"`
	Active       bool      `json:"active"`
	MACAddress   string    `json:"mac_address,omitempty"`
	Interface    string    `json:"interface,omitempty"`
	Broadcast    string    `json:"broadcast,omitempty"`
	Port         int       `json:"port,omitempty"`
	LastDetected time.Time `json:"last_detected_at,omitempty"`
	Reason       string    `json:"reason,omitempty"`
}

type Device struct {
	ID             string
	Name           string
	BootstrapToken string
	AgentToken     string
	Hostname       string
	OSName         string
	IPs            []string
	AgentVersion   string
	LastSeenAt     time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SSHKey struct {
	ID          int64
	Name        string
	PublicKey   string
	Fingerprint string
	CreatedAt   time.Time
}

type Policy struct {
	DeviceID              string
	DeviceMode            DeviceMode
	ManagedAccountEnabled bool
	AccessMode            AccessMode
	SSHKeyID              *int64
	AssignedKeyName       string
	AssignedKey           string
	Fingerprint           string
	PolicyRevision        int64
	UpdatedAt             time.Time
}

type HealthSnapshot struct {
	Hostname     string   `json:"hostname"`
	OSName       string   `json:"os_name"`
	IPs          []string `json:"ips"`
	Uptime       string   `json:"uptime"`
	LoadAverage  string   `json:"load_average"`
	MemoryUsed   string   `json:"memory_used"`
	DiskUsed     string   `json:"disk_used"`
	AgentVersion string   `json:"agent_version"`
	AgentGOOS    string   `json:"agent_goos,omitempty"`
	AgentGOARCH  string   `json:"agent_goarch,omitempty"`
}

type AgentUpdateReport struct {
	Status             AgentUpdateStatus `json:"status"`
	Error              string            `json:"error,omitempty"`
	ServerAgentVersion string            `json:"server_agent_version,omitempty"`
	AttemptedAt        time.Time         `json:"attempted_at,omitempty"`
}

type DeviceReport struct {
	DeviceID               string            `json:"device_id"`
	AppliedRevision        int64             `json:"applied_revision"`
	UserPresent            bool              `json:"user_present"`
	SudoEnabled            bool              `json:"sudo_enabled"`
	DockerEnabled          bool              `json:"docker_enabled"`
	AuditEnabled           bool              `json:"audit_enabled"`
	AuthorizedFingerprints []string          `json:"authorized_fingerprints"`
	EnforcementSucceeded   bool              `json:"enforcement_succeeded"`
	ErrorMessage           string            `json:"error_message"`
	LastPolicyHealth       HealthSnapshot    `json:"health"`
	Topology               TopologyDiscovery `json:"topology"`
	AgentUpdate            AgentUpdateReport `json:"agent_update,omitempty"`
	UpdatedAt              time.Time         `json:"updated_at"`
}

type BootstrapRequest struct {
	BootstrapToken string `json:"bootstrap_token"`
	Hostname       string `json:"hostname"`
	OSName         string `json:"os_name"`
	AgentVersion   string `json:"agent_version"`
}

type BootstrapResponse struct {
	DeviceID   string `json:"device_id"`
	AgentToken string `json:"agent_token"`
	Interval   string `json:"interval"`
}

type CheckInRequest struct {
	Health HealthSnapshot `json:"health"`
}

type ManagedAccountConfig struct {
	ManagedUser   string
	ManagedGroups []string
	AccessMode    AccessMode
}

type ManagedAccountConfigProvider interface {
	ManagedAccountConfig(context.Context) (ManagedAccountConfig, error)
}

type AgentPolicyResponse struct {
	DeviceID              string              `json:"device_id"`
	DeviceMode            DeviceMode          `json:"device_mode"`
	ManagedAccountEnabled bool                `json:"managed_account_enabled"`
	AccessMode            AccessMode          `json:"access_mode"`
	ManagedUser           string              `json:"managed_user"`
	ManagedGroups         []string            `json:"managed_groups,omitempty"`
	SudoersPath           string              `json:"sudoers_path,omitempty"`
	AuditReadmePath       string              `json:"audit_readme_path,omitempty"`
	AuthorizedKeysPath    string              `json:"authorized_keys_path,omitempty"`
	AccountState          string              `json:"account_state"`
	AssignedKeyID         *int64              `json:"assigned_key_id,omitempty"`
	AssignedKey           string              `json:"assigned_public_key"`
	KeyFingerprint        string              `json:"key_fingerprint"`
	PolicyRevision        int64               `json:"policy_revision"`
	FetchedAt             time.Time           `json:"fetched_at"`
	AgentUpdate           AgentUpdateManifest `json:"agent_update"`
}

type AgentUpdateManifest struct {
	Enabled            bool              `json:"enabled"`
	ServerAgentVersion string            `json:"server_agent_version"`
	DownloadURL        string            `json:"download_url,omitempty"`
	SHA256             string            `json:"sha256,omitempty"`
	GOOS               string            `json:"goos,omitempty"`
	GOARCH             string            `json:"goarch,omitempty"`
	Status             AgentUpdateStatus `json:"status,omitempty"`
	Error              string            `json:"error,omitempty"`
}

type DeviceInventoryItem struct {
	Identity   DeviceIdentity      `json:"identity"`
	Connection DeviceConnection    `json:"connection"`
	Topology   DeviceTopologyInfo  `json:"topology"`
	WakeOnLAN  WakeOnLANInfo       `json:"wol"`
	Agent      DeviceAgentInfo     `json:"agent"`
	Access     DeviceAccessInfo    `json:"access"`
	Health     DeviceHealthDetails `json:"health"`
	Inventory  DeviceDiscoveryInfo `json:"inventory"`
}

type DeviceInventoryCompact struct {
	Name     string   `json:"name"`
	Hostname string   `json:"hostname"`
	IPs      []string `json:"ips"`
	Purpose  string   `json:"purpose"`
}

type DeviceInventoryInfo struct {
	Identity   DeviceIdentity     `json:"identity"`
	Connection DeviceConnection   `json:"connection"`
	Topology   DeviceTopologyInfo `json:"topology"`
	WakeOnLAN  WakeOnLANInfo      `json:"wol"`
	Agent      DeviceAgentInfo    `json:"agent"`
	Access     DeviceAccessInfo   `json:"access"`
	Health     DeviceHealthInfo   `json:"health"`
}

type DeviceIdentity struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Hostname   string    `json:"hostname"`
	OSName     string    `json:"os_name"`
	IPs        []string  `json:"ips"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Note       string    `json:"note"`
}

type DeviceConnection struct {
	SSHAlias   string `json:"ssh_alias"`
	SSHCommand string `json:"ssh_command"`
}

type DeviceTopologyInfo struct {
	DeviceType       DeviceType     `json:"device_type"`
	DeviceTypeSource TopologySource `json:"device_type_source"`
	Purpose          DevicePurpose  `json:"purpose"`
	PurposeSource    TopologySource `json:"purpose_source"`
	PlatformClass    PlatformClass  `json:"platform_class"`
	ParentDeviceID   string         `json:"parent_device_id,omitempty"`
	ParentName       string         `json:"parent_name,omitempty"`
	ParentState      ParentState    `json:"parent_state"`
	ParentSource     TopologySource `json:"parent_source"`
	Children         []string       `json:"children,omitempty"`
	LastDiscoveredAt time.Time      `json:"last_discovered_at"`
}

type DeviceAccessInfo struct {
	DeviceMode            DeviceMode `json:"device_mode"`
	ManagedAccountEnabled bool       `json:"managed_account_enabled"`
	ManagedUser           string     `json:"managed_user"`
	AccessMode            AccessMode `json:"access_mode"`
	AssignedKeyName       string     `json:"assigned_key_name"`
	AssignedFingerprint   string     `json:"assigned_fingerprint"`
	PolicyRevision        int64      `json:"policy_revision"`
	AppliedRevision       int64      `json:"applied_revision"`
	EnforcementSucceeded  bool       `json:"enforcement_succeeded"`
	ErrorMessage          string     `json:"error_message"`
}

type DeviceAgentInfo struct {
	Version    string          `json:"version"`
	GOOS       string          `json:"goos,omitempty"`
	GOARCH     string          `json:"goarch,omitempty"`
	AutoUpdate AgentUpdateInfo `json:"auto_update"`
}

type DeviceHealthDetails struct {
	Uptime      string `json:"uptime"`
	LoadAverage string `json:"load_average"`
	MemoryUsed  string `json:"memory_used"`
	DiskUsed    string `json:"disk_used"`
}

type AgentUpdateInfo struct {
	Enabled         bool                    `json:"enabled"`
	Override        AgentAutoUpdateOverride `json:"override"`
	UpdateAvailable bool                    `json:"update_available"`
	ServerVersion   string                  `json:"server_agent_version"`
	Status          AgentUpdateStatus       `json:"status"`
	Error           string                  `json:"error,omitempty"`
	LastCheckedAt   *time.Time              `json:"last_checked_at,omitempty"`
	LastAttemptedAt *time.Time              `json:"last_attempted_at,omitempty"`
}

type DeviceHealthInfo struct {
	Uptime      string `json:"uptime,omitempty"`
	LoadAverage string `json:"load_average,omitempty"`
	MemoryUsed  string `json:"memory_used,omitempty"`
	DiskUsed    string `json:"disk_used,omitempty"`
}

type TopologyGraphCluster struct {
	ID    string           `json:"id"`
	Label string           `json:"label"`
	Kind  TopologyNodeKind `json:"kind,omitempty"`
}

type TopologyGraph struct {
	Nodes    []TopologyGraphNode    `json:"nodes"`
	Links    []TopologyGraphLink    `json:"links"`
	Clusters []TopologyGraphCluster `json:"clusters,omitempty"`
}

type TopologyGraphNode struct {
	ID          string              `json:"id"`
	Kind        TopologyNodeKind    `json:"kind"`
	Label       string              `json:"label"`
	DeviceID    string              `json:"device_id,omitempty"`
	DeviceType  DeviceType          `json:"device_type,omitempty"`
	Purpose     DevicePurpose       `json:"purpose,omitempty"`
	Source      TopologySource      `json:"source"`
	URL         string              `json:"url,omitempty"`
	Note        string              `json:"note,omitempty"`
	Status      *TopologyNodeStatus `json:"status,omitempty"`
	ManualID    int64               `json:"manual_id,omitempty"`
	ParentGroup string              `json:"parent_group,omitempty"`
	ClusterID   string              `json:"cluster_id,omitempty"`
	Position    *TopologyPosition   `json:"position,omitempty"`
}

type TopologyNodeStatus struct {
	LastSeenAt   time.Time `json:"last_seen_at,omitempty"`
	AgentVersion string    `json:"agent_version,omitempty"`
}

type TopologyPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type TopologyGraphLink struct {
	ID       string         `json:"id"`
	From     string         `json:"from"`
	To       string         `json:"to"`
	Label    string         `json:"label,omitempty"`
	Source   TopologySource `json:"source"`
	ManualID int64          `json:"manual_id,omitempty"`
}

type DeviceDiscoveryInfo struct {
	Workloads         []Workload `json:"workloads,omitempty"`
	DiscoveryWarnings []string   `json:"discovery_warnings,omitempty"`
}

type DeviceFindMatch struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Hostname string   `json:"hostname"`
	IPs      []string `json:"ips"`
}

type DeviceFindConflict struct {
	Query   string            `json:"query"`
	Matches []DeviceFindMatch `json:"matches"`
}

type ServiceListItem struct {
	Name       string         `json:"name"`
	Count      int            `json:"count"`
	Healthy    int            `json:"healthy"`
	Unhealthy  int            `json:"unhealthy"`
	Missing    int            `json:"missing"`
	Kinds      []WorkloadKind `json:"kinds"`
	LastSeenAt time.Time      `json:"last_seen_at,omitempty"`
}

type ServiceDeviceSummary struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Hostname string   `json:"hostname"`
	IPs      []string `json:"ips"`
}

type ServiceInstanceInfo struct {
	Name         string               `json:"name"`
	Device       ServiceDeviceSummary `json:"device"`
	Kind         WorkloadKind         `json:"kind"`
	State        string               `json:"state,omitempty"`
	Health       ServiceHealth        `json:"health"`
	Image        string               `json:"image,omitempty"`
	Endpoints    []Endpoint           `json:"endpoints,omitempty"`
	LastSeenAt   time.Time            `json:"last_seen_at,omitempty"`
	MissingSince *time.Time           `json:"missing_since,omitempty"`
}

type ServiceInstanceFull struct {
	ID              int64                `json:"id"`
	NormalizedName  string               `json:"normalized_name"`
	Name            string               `json:"name"`
	Device          ServiceDeviceSummary `json:"device"`
	Kind            WorkloadKind         `json:"kind"`
	State           string               `json:"state,omitempty"`
	DiscoveredState string               `json:"discovered_state,omitempty"`
	Health          ServiceHealth        `json:"health"`
	Image           string               `json:"image,omitempty"`
	Endpoints       []Endpoint           `json:"endpoints,omitempty"`
	FirstSeenAt     time.Time            `json:"first_seen_at,omitempty"`
	LastSeenAt      time.Time            `json:"last_seen_at,omitempty"`
	MissingSince    *time.Time           `json:"missing_since,omitempty"`
	LastReportedAt  time.Time            `json:"last_reported_at,omitempty"`
}
