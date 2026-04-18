package services

import (
	"fmt"
	"os"
	"strings"
	"time"

	"insylus/internal/format"
	"insylus/internal/shared"
)

func PrintServiceListTable(items []shared.ServiceListItem) {
	w := format.NewTable(os.Stdout)
	fmt.Fprintln(w, "SERVICE\tCOUNT\tHEALTH\tKINDS\tLAST SEEN")
	for _, item := range items {
		name := item.Name
		if item.Count > 1 {
			name = fmt.Sprintf("%s (%d)", item.Name, item.Count)
		}
		lastSeen := "never"
		if !item.LastSeenAt.IsZero() {
			lastSeen = item.LastSeenAt.Local().Format(time.DateTime)
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", name, item.Count, serviceHealthSummary(item), workloadKinds(item.Kinds), lastSeen)
	}
	_ = w.Flush()
}

func PrintServiceInfoTable(items []shared.ServiceInstanceInfo) {
	w := format.NewTable(os.Stdout)
	fmt.Fprintln(w, "SERVICE\tKIND\tDEVICE\tHEALTH\tSTATE\tLAST SEEN\tENDPOINTS")
	for _, item := range items {
		lastSeen := "never"
		if !item.LastSeenAt.IsZero() {
			lastSeen = item.LastSeenAt.Local().Format(time.DateTime)
		}
		state := item.State
		if state == "" {
			state = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.Name, item.Kind, item.Device.Name, item.Health, state, lastSeen, endpointsText(item.Endpoints))
	}
	_ = w.Flush()
}

func PrintServiceFullTable(items []shared.ServiceInstanceFull) {
	w := format.NewTable(os.Stdout)
	fmt.Fprintln(w, "SERVICE\tKIND\tDEVICE\tHEALTH\tSTATE\tLAST SEEN\tENDPOINTS")
	for _, item := range items {
		lastSeen := "never"
		if !item.LastSeenAt.IsZero() {
			lastSeen = item.LastSeenAt.Local().Format(time.DateTime)
		}
		state := item.State
		if state == "" {
			state = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.Name, item.Kind, item.Device.Name, item.Health, state, lastSeen, endpointsText(item.Endpoints))
	}
	_ = w.Flush()
}

func serviceHealthSummary(item shared.ServiceListItem) string {
	parts := []string{}
	if item.Healthy > 0 {
		parts = append(parts, fmt.Sprintf("%d healthy", item.Healthy))
	}
	if item.Unhealthy > 0 {
		parts = append(parts, fmt.Sprintf("%d unhealthy", item.Unhealthy))
	}
	if item.Missing > 0 {
		parts = append(parts, fmt.Sprintf("%d missing", item.Missing))
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, ", ")
}

func workloadKinds(kinds []shared.WorkloadKind) string {
	if len(kinds) == 0 {
		return "-"
	}
	values := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		values = append(values, string(kind))
	}
	return strings.Join(values, ",")
}

func endpointsText(endpoints []shared.Endpoint) string {
	if len(endpoints) == 0 {
		return "-"
	}
	values := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.Host != "" {
			values = append(values, endpoint.Host+":"+endpoint.Port)
		} else {
			values = append(values, endpoint.Port)
		}
	}
	return strings.Join(values, ",")
}
