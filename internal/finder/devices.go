package finder

import (
	"insylus/internal/api"
	"insylus/internal/shared"
)

func DeviceByID(client api.Client, id string) (*shared.DeviceInventoryInfo, error) {
	var item shared.DeviceInventoryInfo
	if err := client.DecodeGET(api.AppendView("/api/devices/"+id, "info"), &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func DeviceByName(client api.Client, name string) (*shared.DeviceInventoryInfo, error) {
	return FindDevice(client, name)
}

func FindDevice(client api.Client, query string) (*shared.DeviceInventoryInfo, error) {
	var item shared.DeviceInventoryInfo
	if err := client.DecodeGET("/api/devices/find?q="+api.URLQueryEscape(query)+"&view=info", &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func FindDevices(client api.Client, query string) ([]shared.DeviceInventoryInfo, error) {
	var item shared.DeviceInventoryInfo
	if err := client.DecodeGET("/api/devices/find?q="+api.URLQueryEscape(query)+"&view=info", &item); err != nil {
		return nil, err
	}
	return []shared.DeviceInventoryInfo{item}, nil
}
