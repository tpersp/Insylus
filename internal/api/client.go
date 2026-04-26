package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"insylus/internal/shared"
)

const (
	DefaultTimeout    = 20 * time.Second
	MaxErrorBodyBytes = 64 * 1024
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func DefaultServerURL() string {
	if value := strings.TrimSpace(os.Getenv("INSYLUS_SERVER_URL")); value != "" {
		return value
	}
	return "http://127.0.0.1:8080"
}

func NewClient(baseURL string) Client {
	return Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: &http.Client{Timeout: DefaultTimeout}}
}

func (c Client) Get(path string) (*http.Response, error) {
	return c.httpClient().Get(c.BaseURL + path)
}

func (c Client) Post(path, contentType string, body io.Reader) (*http.Response, error) {
	return c.httpClient().Post(c.BaseURL+path, contentType, body)
}

func (c Client) DecodeGET(path string, dst any) error {
	resp, err := c.Get(path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		HandleErrorResponse(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}
	return nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: DefaultTimeout}
}

func PrintRawJSON(r io.Reader) {
	data, err := io.ReadAll(r)
	if err != nil {
		Fatalf("read failed: %v", err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		Fatalf("decode failed: %v", err)
	}
	fmt.Fprintln(os.Stdout, out.String())
}

func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func AppendView(path, view string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "view=" + view
}

func URLQueryEscape(v string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		" ", "%20",
		"+", "%2B",
		"&", "%26",
		"?", "%3F",
		"#", "%23",
		"=", "%3D",
		"/", "%2F",
	)
	return replacer.Replace(v)
}

func HandleErrorResponse(resp *http.Response) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorBodyBytes))
	if resp.StatusCode == http.StatusConflict {
		var conflict shared.DeviceFindConflict
		if err := json.Unmarshal(body, &conflict); err == nil && len(conflict.Matches) > 0 {
			fmt.Fprintf(os.Stderr, "find query %q matched multiple devices:\n", conflict.Query)
			for _, match := range conflict.Matches {
				fmt.Fprintf(os.Stderr, "  %s (%s) %v [%s]\n", match.Name, match.Hostname, match.IPs, match.ID)
			}
			os.Exit(1)
		}
	}
	Fatalf("request failed: %s: %s", resp.Status, string(body))
}
