package homebox

import "time"

type config struct {
	BaseURL         string
	Username        string
	Password        string
	Token           string
	AttachmentToken string
	ExpiresAt       *time.Time
	LastConnectedAt *time.Time
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type configSummary struct {
	BaseURL         string     `json:"base_url,omitempty"`
	Username        string     `json:"username,omitempty"`
	Configured      bool       `json:"configured"`
	Connected       bool       `json:"connected"`
	TokenExpiresAt  *time.Time `json:"token_expires_at,omitempty"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

type loginResponse struct {
	Token           string `json:"token"`
	AttachmentToken string `json:"attachmentToken"`
	ExpiresAt       string `json:"expiresAt"`
}

type authState struct {
	Token           string
	AttachmentToken string
	ExpiresAt       *time.Time
}

type selfResponse struct {
	Item map[string]any `json:"item,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

type connectionTestResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	User    any    `json:"user,omitempty"`
}
