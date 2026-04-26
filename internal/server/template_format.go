package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func formatUptime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		seconds, parseErr := strconv.ParseFloat(strings.TrimSuffix(raw, "s"), 64)
		if parseErr != nil {
			return raw
		}
		duration = time.Duration(seconds * float64(time.Second))
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
