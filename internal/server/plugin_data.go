package server

import (
	"context"
	"database/sql"
	"strings"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type serverDataHost struct {
	app *App
}

type serverInventoryService struct {
	app *App
}

func (h serverDataHost) Inventory() pluginhost.InventoryService {
	return serverInventoryService{app: h.app}
}

func (s serverInventoryService) ListDevices(ctx context.Context) ([]pluginhost.InventoryDevice, error) {
	targets, err := s.app.store.targetService().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]pluginhost.InventoryDevice, 0, len(targets))
	for _, target := range targets {
		out = append(out, inventoryDeviceFromTarget(target))
	}
	return out, nil
}

func (s serverInventoryService) GetDevice(ctx context.Context, id string) (pluginhost.InventoryDevice, error) {
	target, err := s.app.store.targetService().Get(ctx, id)
	if err != nil {
		return pluginhost.InventoryDevice{}, err
	}
	return inventoryDeviceFromTarget(target), nil
}

func (s serverInventoryService) FindDevice(ctx context.Context, query string) ([]pluginhost.InventoryDevice, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	devices, err := s.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	var matches []pluginhost.InventoryDevice
	for _, device := range devices {
		if strings.EqualFold(device.ID, query) ||
			strings.EqualFold(device.Name, query) ||
			strings.EqualFold(device.Hostname, query) ||
			hasInventoryIP(device.IPs, query) {
			matches = append(matches, device)
		}
	}
	if len(matches) == 0 {
		return nil, sql.ErrNoRows
	}
	return matches, nil
}

func (s serverInventoryService) ListInventory(ctx context.Context, view, managedUser string) (any, error) {
	records, err := s.app.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	children := childNamesByParent(records)
	switch inventoryViewFromString(view, inventoryViewCompact) {
	case inventoryViewCompact:
		items := make([]shared.DeviceInventoryCompact, 0, len(records))
		for _, record := range records {
			items = append(items, InventoryCompactFromRecord(record))
		}
		return items, nil
	case inventoryViewInfo:
		items := make([]shared.DeviceInventoryInfo, 0, len(records))
		for _, record := range records {
			items = append(items, InventoryInfoFromRecordWithManagedUser(record, children[record.Device.ID], managedUser))
		}
		return items, nil
	default:
		items := make([]shared.DeviceInventoryItem, 0, len(records))
		for _, record := range records {
			items = append(items, InventoryItemFromRecordWithManagedUser(record, children[record.Device.ID], managedUser))
		}
		return items, nil
	}
}

func (s serverInventoryService) GetInventory(ctx context.Context, id, view, managedUser string) (any, error) {
	record, err := s.app.store.GetDevice(ctx, id)
	if err != nil {
		return nil, err
	}
	records, err := s.app.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	children := childNamesByParent(records)
	return inventoryByView(record, children[record.Device.ID], inventoryViewFromString(view, inventoryViewInfo), managedUser), nil
}

func (s serverInventoryService) FindInventory(ctx context.Context, query, view, managedUser string) (any, error) {
	records, err := s.app.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	matches := findMatchingDevices(records, query)
	if len(matches) == 0 {
		return nil, sql.ErrNoRows
	}
	if len(matches) > 1 {
		conflict := shared.DeviceFindConflict{Query: query}
		for _, record := range matches {
			conflict.Matches = append(conflict.Matches, FindMatchFromRecord(record))
		}
		return conflict, pluginhost.ErrInventoryFindConflict
	}
	children := childNamesByParent(records)
	return inventoryByView(matches[0], children[matches[0].Device.ID], inventoryViewFromString(view, inventoryViewInfo), managedUser), nil
}

func inventoryViewFromString(raw string, fallback inventoryView) inventoryView {
	switch raw {
	case string(inventoryViewCompact):
		return inventoryViewCompact
	case string(inventoryViewInfo):
		return inventoryViewInfo
	case string(inventoryViewFull):
		return inventoryViewFull
	default:
		return fallback
	}
}

func inventoryByView(record DeviceRecord, children []string, view inventoryView, managedUser string) any {
	switch view {
	case inventoryViewCompact:
		return InventoryCompactFromRecord(record)
	case inventoryViewInfo:
		return InventoryInfoFromRecordWithManagedUser(record, children, managedUser)
	default:
		return InventoryItemFromRecordWithManagedUser(record, children, managedUser)
	}
}

func inventoryDeviceFromRecord(record DeviceRecord) pluginhost.InventoryDevice {
	return pluginhost.InventoryDevice{
		ID:             record.Device.ID,
		Name:           record.Device.Name,
		Hostname:       record.Device.Hostname,
		OSName:         record.Device.OSName,
		IPs:            append([]string(nil), record.Device.IPs...),
		LastSeenAt:     record.Device.LastSeenAt,
		DeviceType:     string(record.Resolved.EffectiveDeviceType),
		Purpose:        string(record.Resolved.Purpose),
		DiscoveredType: string(record.Discovery.DeviceType),
		DiscoveredRole: string(record.Discovery.Purpose),
		WakeOnLAN:      record.Discovery.WakeOnLAN,
	}
}

func inventoryDeviceFromTarget(target pluginhost.Target) pluginhost.InventoryDevice {
	return pluginhost.InventoryDevice{
		ID:         target.ID,
		Name:       target.Name,
		Hostname:   target.Hostname,
		IPs:        append([]string(nil), target.IPs...),
		LastSeenAt: target.UpdatedAt,
		DeviceType: target.Kind,
		Purpose:    target.Kind,
	}
}

func hasInventoryIP(ips []string, query string) bool {
	for _, ip := range ips {
		if ip == query {
			return true
		}
	}
	return false
}
