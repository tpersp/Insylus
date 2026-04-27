package homebox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	got := normalizeBaseURL("  http://example.test:7745/// ")
	if got != "http://example.test:7745" {
		t.Fatalf("normalizeBaseURL() = %q", got)
	}
}

func TestClientRefreshesTokenBeforeRequest(t *testing.T) {
	loginCount := 0
	apiCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/login":
			loginCount++
			if r.Method != http.MethodPost {
				t.Fatalf("login method = %s", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Fatalf("login content type = %q", got)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.FormValue("username") != "me@example.test" || r.FormValue("password") != "secret" || r.FormValue("stayLoggedIn") != "true" {
				t.Fatalf("unexpected login form: %#v", r.Form)
			}
			expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"Bearer fresh","attachmentToken":"attach","expiresAt":"` + expires + `"}`))
		case "/api/v1/users/self":
			apiCount++
			if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
				t.Fatalf("authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"item":{"name":"Me"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var saved authState
	client, err := NewClient(config{
		BaseURL:   server.URL + "/",
		Username:  "me@example.test",
		Password:  "secret",
		Token:     "old",
		ExpiresAt: ptrTime(time.Now().UTC().Add(2 * time.Minute)),
	}, func(state authState) error {
		saved = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := client.GetJSON(context.Background(), "/v1/users/self", &out); err != nil {
		t.Fatal(err)
	}
	if loginCount != 1 || apiCount != 1 {
		t.Fatalf("loginCount=%d apiCount=%d", loginCount, apiCount)
	}
	if saved.Token != "fresh" || saved.AttachmentToken != "attach" || saved.ExpiresAt == nil {
		t.Fatalf("auth state not saved: %#v", saved)
	}
}

func TestClientRetriesOnceAfterUnauthorized(t *testing.T) {
	loginCount := 0
	apiCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/login":
			loginCount++
			expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"Bearer fresh","expiresAt":"` + expires + `"}`))
		case "/api/v1/items":
			apiCount++
			if apiCount == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
				t.Fatalf("authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(config{
		BaseURL:   server.URL,
		Username:  "me",
		Password:  "secret",
		Token:     "stale",
		ExpiresAt: ptrTime(time.Now().UTC().Add(time.Hour)),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := client.GetJSON(context.Background(), "/v1/items", &out); err != nil {
		t.Fatal(err)
	}
	if loginCount != 1 || apiCount != 2 {
		t.Fatalf("loginCount=%d apiCount=%d", loginCount, apiCount)
	}
}

func TestAuthorizationHeaderAcceptsRawOrPrefixedToken(t *testing.T) {
	if got := authorizationHeader("fresh"); got != "Bearer fresh" {
		t.Fatalf("raw token header = %q", got)
	}
	if got := authorizationHeader("Bearer fresh"); got != "Bearer fresh" {
		t.Fatalf("prefixed token header = %q", got)
	}
	if got := normalizeAuthToken("Bearer fresh"); got != "fresh" {
		t.Fatalf("normalized token = %q", got)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
