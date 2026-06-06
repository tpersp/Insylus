package model

import "time"

type Server struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Hostname  string    `json:"hostname"`
	Address   string    `json:"address"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Principal struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AccessGrant struct {
	ID            int64     `json:"id"`
	ServerID      int64     `json:"server_id"`
	ServerName    string    `json:"server_name,omitempty"`
	PrincipalID   int64     `json:"principal_id"`
	PrincipalName string    `json:"principal_name,omitempty"`
	Account       string    `json:"account"`
	Sudo          string    `json:"sudo"`
	Notes         string    `json:"notes,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

const (
	PrincipalHuman   = "human"
	PrincipalService = "service"
	PrincipalAIAgent = "ai-agent"

	SudoNone         = "none"
	SudoPrompted     = "prompted"
	SudoPasswordless = "passwordless"
)
