package homebox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errHomeBoxAuth = errors.New("homebox authentication failed")

type Client struct {
	baseURL         string
	username        string
	password        string
	token           string
	attachmentToken string
	expiresAt       *time.Time
	httpClient      *http.Client
	onAuth          func(authState) error
}

func NewClient(cfg config, onAuth func(authState) error) (*Client, error) {
	baseURL := normalizeBaseURL(cfg.BaseURL)
	if baseURL == "" {
		return nil, errors.New("base_url is required")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New("base_url must start with http:// or https://")
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return nil, errors.New("username is required")
	}
	if cfg.Password == "" {
		return nil, errors.New("password is required")
	}
	return &Client{
		baseURL:         baseURL,
		username:        strings.TrimSpace(cfg.Username),
		password:        cfg.Password,
		token:           cfg.Token,
		attachmentToken: cfg.AttachmentToken,
		expiresAt:       cfg.ExpiresAt,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		onAuth:          onAuth,
	}, nil
}

func normalizeBaseURL(input string) string {
	return strings.TrimRight(strings.TrimSpace(input), "/")
}

func (c *Client) apiURL(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + "/api" + path
}

func (c *Client) Login(ctx context.Context) error {
	values := url.Values{}
	values.Set("username", c.username)
	values.Set("password", c.password)
	values.Set("stayLoggedIn", "true")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL("/v1/users/login"), strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Cannot reach HomeBox: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: Invalid credentials", errHomeBoxAuth)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Auth failed: HomeBox returned %s", resp.Status)
	}
	var login loginResponse
	if err := json.Unmarshal(data, &login); err != nil {
		return fmt.Errorf("Unexpected API response: %w", err)
	}
	if login.Token == "" {
		return errors.New("Unexpected API response: missing token")
	}
	expiresAt, err := parseHomeBoxTime(login.ExpiresAt)
	if err != nil {
		return fmt.Errorf("Unexpected API response: invalid expiresAt")
	}
	c.token = normalizeAuthToken(login.Token)
	c.attachmentToken = login.AttachmentToken
	c.expiresAt = expiresAt
	if c.onAuth != nil {
		if err := c.onAuth(authState{Token: c.token, AttachmentToken: c.attachmentToken, ExpiresAt: c.expiresAt}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) tokenNeedsRefresh() bool {
	if strings.TrimSpace(c.token) == "" {
		return true
	}
	if c.expiresAt == nil || c.expiresAt.IsZero() {
		return true
	}
	return time.Now().UTC().After(c.expiresAt.Add(-10 * time.Minute))
}

func (c *Client) ensureAuthenticated(ctx context.Context) error {
	if c.tokenNeedsRefresh() {
		return c.Login(ctx)
	}
	return nil
}

func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.RequestJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) RequestJSON(ctx context.Context, method, path string, body any, out any) error {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return err
	}
	data, status, err := c.doJSON(ctx, method, path, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		if err := c.Login(ctx); err != nil {
			return err
		}
		data, status, err = c.doJSON(ctx, method, path, body)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return fmt.Errorf("%w: HomeBox rejected refreshed credentials", errHomeBoxAuth)
		}
	}
	if status < 200 || status >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = http.StatusText(status)
		}
		return fmt.Errorf("HomeBox API returned %d: %s", status, msg)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("Unexpected API response: %w", err)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL(path), reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", authorizationHeader(c.token))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Cannot reach HomeBox: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return data, resp.StatusCode, nil
}

func normalizeAuthToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) >= 7 && strings.EqualFold(token[:7], "Bearer ") {
		return strings.TrimSpace(token[7:])
	}
	return token
}

func authorizationHeader(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) >= 7 && strings.EqualFold(token[:7], "Bearer ") {
		return token
	}
	return "Bearer " + token
}

func parseHomeBoxTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			utc := t.UTC()
			return &utc, nil
		}
	}
	return nil, errors.New("invalid time")
}
