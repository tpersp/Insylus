package dashboard

import (
	"database/sql"
	"net/http"
	"sort"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type runtime struct {
	db        pluginhost.DBHost
	inventory pluginhost.InventoryService
	plugins   pluginhost.PluginRegistry
	targets   pluginhost.TargetService
	render    func(http.ResponseWriter, string, any)
}

func (rt runtime) handlePage(w http.ResponseWriter, r *http.Request) {
	data, err := rt.pageData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "dashboard.html", data)
}

func (rt runtime) pageData(r *http.Request) (pageData, error) {
	var data pageData
	plugins := rt.plugins.Available()
	data.Plugins = pluginViews(plugins)
	data.SupportingState = supportingStateFromPlugins(plugins)

	devices, err := rt.inventory.ListInventory(r.Context(), "full", "")
	if err != nil {
		return data, err
	}
	items, _ := devices.([]shared.DeviceInventoryItem)
	data.Summary = summarizeDevices(items, time.Now())
	data.ProblemDevices = problemDevices(items)
	data.RecentDevices = recentDevices(items)

	serviceSummary, err := rt.serviceSummary(r)
	if err != nil {
		return data, err
	}
	data.ServiceSummary = serviceSummary
	data.Summary.HealthyServices = serviceSummary.Healthy
	data.Summary.UnhealthyServices = serviceSummary.Unhealthy
	data.Summary.MissingServices = serviceSummary.Missing
	data.Summary.UnknownServices = serviceSummary.Unknown

	events, err := rt.recentEvents(r, 8)
	if err != nil {
		return data, err
	}
	data.RecentEvents = events
	return data, nil
}

func summarizeDevices(devices []shared.DeviceInventoryItem, now time.Time) fleetSummary {
	var summary fleetSummary
	for _, device := range devices {
		summary.TotalDevices++
		switch device.Access.DeviceMode {
		case shared.DeviceModeAccessManaged:
			summary.AccessManagedDevices++
		default:
			summary.InventoryOnlyDevices++
		}
		if device.Agent.AutoUpdate.UpdateAvailable {
			summary.AgentUpdatesAvailable++
		}
		if device.Agent.AutoUpdate.Status == shared.AgentUpdateStatusFailed {
			summary.AgentUpdateFailures++
		}
		switch freshness(device.Identity.LastSeenAt, now) {
		case "online":
			summary.OnlineDevices++
		case "stale":
			summary.StaleDevices++
		default:
			summary.OfflineDevices++
		}
	}
	return summary
}

func problemDevices(devices []shared.DeviceInventoryItem) []shared.DeviceInventoryItem {
	out := []shared.DeviceInventoryItem{}
	now := time.Now()
	for _, device := range devices {
		accessProblem := device.Access.ErrorMessage != "" ||
			(device.Access.DeviceMode == shared.DeviceModeAccessManaged && !device.Access.EnforcementSucceeded)
		if freshness(device.Identity.LastSeenAt, now) != "online" ||
			device.Agent.AutoUpdate.UpdateAvailable ||
			device.Agent.AutoUpdate.Status == shared.AgentUpdateStatusFailed ||
			accessProblem {
			out = append(out, device)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Identity.LastSeenAt.Before(out[j].Identity.LastSeenAt)
	})
	if len(out) > 8 {
		return out[:8]
	}
	return out
}

func recentDevices(devices []shared.DeviceInventoryItem) []shared.DeviceInventoryItem {
	out := append([]shared.DeviceInventoryItem(nil), devices...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Identity.LastSeenAt.After(out[j].Identity.LastSeenAt)
	})
	if len(out) > 6 {
		return out[:6]
	}
	return out
}

func freshness(lastSeen time.Time, now time.Time) string {
	if lastSeen.IsZero() {
		return "offline"
	}
	if now.Sub(lastSeen) <= shared.DeviceOnlineWindow {
		return "online"
	}
	if now.Sub(lastSeen) <= 24*time.Hour {
		return "stale"
	}
	return "offline"
}

func (rt runtime) serviceSummary(r *http.Request) (serviceSummary, error) {
	var summary serviceSummary
	rows, err := rt.db.QueryContext(r.Context(), `
		select kind, health, count(*)
		from service_instances
		group by kind, health
		order by kind, health`)
	if err != nil {
		return summary, err
	}
	defer rows.Close()
	kindCounts := map[shared.WorkloadKind]int{}
	for rows.Next() {
		var kind shared.WorkloadKind
		var health shared.ServiceHealth
		var count int
		if err := rows.Scan(&kind, &health, &count); err != nil {
			return summary, err
		}
		summary.Total += count
		kindCounts[kind] += count
		switch health {
		case shared.ServiceHealthHealthy:
			summary.Healthy += count
		case shared.ServiceHealthUnhealthy:
			summary.Unhealthy += count
		case shared.ServiceHealthMissing:
			summary.Missing += count
		default:
			summary.Unknown += count
		}
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}
	for kind, count := range kindCounts {
		summary.ByKind = append(summary.ByKind, serviceKindCount{Kind: kind, Count: count})
	}
	sort.Slice(summary.ByKind, func(i, j int) bool {
		return summary.ByKind[i].Kind < summary.ByKind[j].Kind
	})
	hotList, err := rt.serviceHotList(r)
	if err != nil {
		return summary, err
	}
	summary.HotList = hotList
	summary.HasRecords = summary.Total > 0
	return summary, nil
}

func (rt runtime) serviceHotList(r *http.Request) ([]serviceHealthItem, error) {
	rows, err := rt.db.QueryContext(r.Context(), `
		select s.name, s.kind, s.health, s.state, s.last_seen_at, d.id, d.name
		from service_instances s
		join devices d on d.id = s.device_id
		where s.health in ('unhealthy', 'missing')
		order by
			case s.health when 'unhealthy' then 0 else 1 end,
			coalesce(s.last_seen_at, '') desc,
			s.name asc
		limit 8`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []serviceHealthItem
	for rows.Next() {
		var item serviceHealthItem
		var lastSeen sql.NullString
		if err := rows.Scan(&item.Name, &item.Kind, &item.Health, &item.State, &lastSeen, &item.DeviceID, &item.DeviceName); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			item.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeen.String)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (rt runtime) recentEvents(r *http.Request, limit int) ([]serviceEvent, error) {
	rows, err := rt.db.QueryContext(r.Context(), `
		select e.action, e.service_name, e.service_kind, e.details, e.created_at, d.id, d.name
		from service_events e
		join devices d on d.id = e.device_id
		order by e.created_at desc, e.id desc
		limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []serviceEvent
	for rows.Next() {
		var event serviceEvent
		var created string
		if err := rows.Scan(&event.Action, &event.ServiceName, &event.ServiceKind, &event.Details, &created, &event.DeviceID, &event.DeviceName); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, event)
	}
	return out, rows.Err()
}

func pluginViews(manifests []pluginhost.PluginManifest) []pluginView {
	out := make([]pluginView, 0, len(manifests))
	for _, manifest := range manifests {
		out = append(out, pluginView{PluginManifest: manifest, Href: pluginHref(manifest.ID)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > 8 {
		return out[:8]
	}
	return out
}

func supportingStateFromPlugins(manifests []pluginhost.PluginManifest) supportingState {
	var state supportingState
	for _, manifest := range manifests {
		switch manifest.ID {
		case "devices":
			state.DevicesEnabled = manifest.Enabled
		case "services":
			state.ServicesEnabled = manifest.Enabled
		case "topology":
			state.TopologyEnabled = manifest.Enabled
		case "access":
			state.AccessEnabled = manifest.Enabled
		}
	}
	return state
}

func pluginHref(id string) string {
	switch id {
	case "dashboard":
		return "/"
	case "devices", "agent":
		return "/devices"
	case "services":
		return "/services"
	case "topology":
		return "/topology"
	case "access":
		return "/keys"
	case "docker":
		return "/docker"
	case "jellyfin":
		return "/jellyfin"
	case "proxmox":
		return "/proxmox"
	default:
		return "/plugins"
	}
}
