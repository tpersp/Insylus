package api

import (
	"net/http"
	"testing"
)

func TestNewClientUsesBoundedTimeout(t *testing.T) {
	client := NewClient("http://127.0.0.1:8080/")

	if client.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("BaseURL = %q", client.BaseURL)
	}
	if client.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}
	if client.HTTPClient.Timeout != DefaultTimeout {
		t.Fatalf("Timeout = %s, want %s", client.HTTPClient.Timeout, DefaultTimeout)
	}
}

func TestClientHonorsInjectedHTTPClient(t *testing.T) {
	custom := &http.Client{}
	client := Client{BaseURL: "http://example.test", HTTPClient: custom}

	if got := client.httpClient(); got != custom {
		t.Fatal("httpClient did not return injected client")
	}
}
