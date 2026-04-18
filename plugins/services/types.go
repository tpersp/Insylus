package services

import (
	"time"

	"insylus/internal/shared"
)

type serviceInstanceRecord struct {
	ID              int64
	Device          shared.Device
	NormalizedName  string
	Name            string
	Kind            shared.WorkloadKind
	Image           string
	State           string
	DiscoveredState string
	Health          shared.ServiceHealth
	Endpoints       []shared.Endpoint
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	MissingSince    time.Time
	LastReportedAt  time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type serviceEventRecord struct {
	ID          int64
	Action      string
	ServiceName string
	ServiceKind shared.WorkloadKind
	Device      shared.Device
	Details     string
	CreatedAt   time.Time
}

type servicesPageData struct {
	Services []serviceInstanceRecord
	Groups   []shared.ServiceListItem
	Events   []serviceEventRecord
}

type historyPageData struct {
	Events []serviceEventRecord
}
