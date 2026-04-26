package server

import "testing"

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "", want: "unknown"},
		{raw: "45s", want: "45s"},
		{raw: "1229802.04s", want: "14d 5h 36m"},
		{raw: "2h15m10s", want: "2h 15m"},
		{raw: "not-a-duration", want: "not-a-duration"},
	}
	for _, tt := range tests {
		if got := formatUptime(tt.raw); got != tt.want {
			t.Fatalf("formatUptime(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
