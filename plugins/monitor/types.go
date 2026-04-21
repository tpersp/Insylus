package monitor

import "time"

type Settings struct {
	IntervalSeconds int `json:"interval_seconds"`
	TimeoutMillis   int `json:"timeout_millis"`
}

type ManualTarget struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type Status struct {
	Key             string    `json:"key"`
	Source          string    `json:"source"`
	DeviceID        string    `json:"device_id,omitempty"`
	ManualTargetID  int64     `json:"manual_target_id,omitempty"`
	Name            string    `json:"name"`
	Host            string    `json:"host"`
	Port            int       `json:"port,omitempty"`
	Enabled         bool      `json:"enabled"`
	State           string    `json:"state"`
	LatencyMs       float64   `json:"latency_ms,omitempty"`
	Availability24h float64   `json:"availability_24h"`
	LastCheckedAt   time.Time `json:"last_checked_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	Samples24h      int       `json:"samples_24h"`
	Successes24h    int       `json:"successes_24h"`
	MonitorMethod   string    `json:"monitor_method"`
	HasRecentData   bool      `json:"has_recent_data"`
}

type HistoryPoint struct {
	CheckedAt time.Time `json:"checked_at"`
	Success   bool      `json:"success"`
	LatencyMs float64   `json:"latency_ms,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type HistoryResponse struct {
	Key    string         `json:"key"`
	Window string         `json:"window"`
	Points []HistoryPoint `json:"points"`
	Target Status         `json:"target"`
}

type monitorTarget struct {
	Key            string
	Source         string
	DeviceID       string
	ManualTargetID int64
	Name           string
	Host           string
	Port           int
	Enabled        bool
	MonitorMethod  string
}

type sampleRecord struct {
	Key       string
	Name      string
	Source    string
	DeviceID  string
	Host      string
	Port      int
	Success   bool
	LatencyMs float64
	Error     string
	CheckedAt time.Time
}
