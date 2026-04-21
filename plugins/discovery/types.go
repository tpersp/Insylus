package discovery

import "time"

const (
	statusPending  = "pending"
	statusIgnored  = "ignored"
	statusPromoted = "promoted"
)

type candidate struct {
	ID               int64     `json:"id"`
	DisplayName      string    `json:"display_name"`
	Hostname         string    `json:"hostname,omitempty"`
	IPAddress        string    `json:"ip_address"`
	MACAddress       string    `json:"mac_address,omitempty"`
	OpenPorts        []int     `json:"open_ports,omitempty"`
	Status           string    `json:"status"`
	StatusNote       string    `json:"status_note,omitempty"`
	SourceCIDR       string    `json:"source_cidr,omitempty"`
	KindHint         string    `json:"kind_hint,omitempty"`
	PromotedTargetID string    `json:"promoted_target_id,omitempty"`
	FirstSeenAt      time.Time `json:"first_seen_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type scanRequest struct {
	CIDR string `json:"cidr"`
}

type scanResponse struct {
	CIDR       string      `json:"cidr"`
	Scanned    int         `json:"scanned"`
	Discovered int         `json:"discovered"`
	Candidates []candidate `json:"candidates"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type promoteResponse struct {
	Candidate candidate `json:"candidate"`
	TargetID  string    `json:"target_id"`
}

type scanResult struct {
	DisplayName string
	Hostname    string
	IPAddress   string
	MACAddress  string
	OpenPorts   []int
	KindHint    string
}
