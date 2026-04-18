package services

import (
	"sort"
	"strings"
	"time"

	"insylus/internal/shared"
)

func serviceListFromRecords(records []serviceInstanceRecord) []shared.ServiceListItem {
	type group struct {
		item  shared.ServiceListItem
		kinds map[shared.WorkloadKind]struct{}
	}
	groups := map[string]*group{}
	for _, record := range records {
		key := record.NormalizedName
		if key == "" {
			key = normalizeServiceName(record.Name)
		}
		g, ok := groups[key]
		if !ok {
			g = &group{
				item:  shared.ServiceListItem{Name: record.Name},
				kinds: map[shared.WorkloadKind]struct{}{},
			}
			groups[key] = g
		}
		g.item.Count++
		switch record.Health {
		case shared.ServiceHealthHealthy:
			g.item.Healthy++
		case shared.ServiceHealthMissing:
			g.item.Missing++
		case shared.ServiceHealthUnhealthy:
			g.item.Unhealthy++
		}
		if record.Kind != "" {
			g.kinds[record.Kind] = struct{}{}
		}
		if record.LastSeenAt.After(g.item.LastSeenAt) {
			g.item.LastSeenAt = record.LastSeenAt
		}
	}

	items := make([]shared.ServiceListItem, 0, len(groups))
	for _, g := range groups {
		for kind := range g.kinds {
			g.item.Kinds = append(g.item.Kinds, kind)
		}
		sort.Slice(g.item.Kinds, func(i, j int) bool {
			return g.item.Kinds[i] < g.item.Kinds[j]
		})
		items = append(items, g.item)
	}
	sort.Slice(items, func(i, j int) bool {
		return normalizeServiceName(items[i].Name) < normalizeServiceName(items[j].Name)
	})
	return items
}

func serviceInfoFromRecord(record serviceInstanceRecord) shared.ServiceInstanceInfo {
	return shared.ServiceInstanceInfo{
		Name:         record.Name,
		Device:       serviceDeviceSummary(record.Device),
		Kind:         record.Kind,
		State:        record.State,
		Health:       record.Health,
		Image:        record.Image,
		Endpoints:    record.Endpoints,
		LastSeenAt:   record.LastSeenAt,
		MissingSince: serviceTimePtr(record.MissingSince),
	}
}

func serviceFullFromRecord(record serviceInstanceRecord) shared.ServiceInstanceFull {
	return shared.ServiceInstanceFull{
		ID:              record.ID,
		NormalizedName:  record.NormalizedName,
		Name:            record.Name,
		Device:          serviceDeviceSummary(record.Device),
		Kind:            record.Kind,
		State:           record.State,
		DiscoveredState: record.DiscoveredState,
		Health:          record.Health,
		Image:           record.Image,
		Endpoints:       record.Endpoints,
		FirstSeenAt:     record.FirstSeenAt,
		LastSeenAt:      record.LastSeenAt,
		MissingSince:    serviceTimePtr(record.MissingSince),
		LastReportedAt:  record.LastReportedAt,
	}
}

func serviceInfosFromRecords(records []serviceInstanceRecord) []shared.ServiceInstanceInfo {
	items := make([]shared.ServiceInstanceInfo, 0, len(records))
	for _, record := range records {
		items = append(items, serviceInfoFromRecord(record))
	}
	return items
}

func serviceFullsFromRecords(records []serviceInstanceRecord) []shared.ServiceInstanceFull {
	items := make([]shared.ServiceInstanceFull, 0, len(records))
	for _, record := range records {
		items = append(items, serviceFullFromRecord(record))
	}
	return items
}

func serviceDeviceSummary(device shared.Device) shared.ServiceDeviceSummary {
	return shared.ServiceDeviceSummary{
		ID:       device.ID,
		Name:     device.Name,
		Hostname: device.Hostname,
		IPs:      device.IPs,
	}
}

func serviceTimePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func normalizeServiceName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}
