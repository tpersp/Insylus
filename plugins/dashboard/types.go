package dashboard

import (
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type pageData struct {
	Summary         fleetSummary
	ProblemDevices  []shared.DeviceInventoryItem
	RecentDevices   []shared.DeviceInventoryItem
	ServiceSummary  serviceSummary
	RecentEvents    []serviceEvent
	Plugins         []pluginView
	SupportingState supportingState
}

type fleetSummary struct {
	TotalDevices          int
	OnlineDevices         int
	StaleDevices          int
	OfflineDevices        int
	InventoryOnlyDevices  int
	AccessManagedDevices  int
	AgentUpdatesAvailable int
	AgentUpdateFailures   int
	HealthyServices       int
	UnhealthyServices     int
	MissingServices       int
	UnknownServices       int
}

type serviceSummary struct {
	Total      int
	Healthy    int
	Unhealthy  int
	Missing    int
	Unknown    int
	ByKind     []serviceKindCount
	HotList    []serviceHealthItem
	HasRecords bool
}

type serviceKindCount struct {
	Kind  shared.WorkloadKind
	Count int
}

type serviceHealthItem struct {
	Name       string
	DeviceID   string
	DeviceName string
	Kind       shared.WorkloadKind
	Health     shared.ServiceHealth
	State      string
	LastSeenAt time.Time
}

type serviceEvent struct {
	Action      string
	ServiceName string
	ServiceKind shared.WorkloadKind
	DeviceID    string
	DeviceName  string
	Details     string
	CreatedAt   time.Time
}

type pluginView struct {
	pluginhost.PluginManifest
	Href string
}

type supportingState struct {
	DevicesEnabled  bool
	ServicesEnabled bool
	TopologyEnabled bool
	AccessEnabled   bool
}
