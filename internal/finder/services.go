package finder

import (
	"insylus/internal/api"
	"insylus/internal/shared"
)

func FindServices(client api.Client, query string) ([]shared.ServiceInstanceInfo, error) {
	var items []shared.ServiceInstanceInfo
	if err := client.DecodeGET("/api/services/find?q="+api.URLQueryEscape(query)+"&view=info", &items); err != nil {
		return nil, err
	}
	return items, nil
}

func ServicesForDevice(client api.Client, query string) ([]shared.ServiceInstanceInfo, error) {
	var items []shared.ServiceInstanceInfo
	if err := client.DecodeGET("/api/services?device="+api.URLQueryEscape(query)+"&view=info", &items); err != nil {
		return nil, err
	}
	return items, nil
}
