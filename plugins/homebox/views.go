package homebox

import (
	"fmt"
	"net/http"
	"strings"
)

type homeBoxView string

const (
	homeBoxViewCompact homeBoxView = "compact"
	homeBoxViewInfo    homeBoxView = "info"
	homeBoxViewFull    homeBoxView = "full"
)

func parseHomeBoxView(w http.ResponseWriter, r *http.Request) (homeBoxView, bool) {
	view := homeBoxView(strings.TrimSpace(r.URL.Query().Get("view")))
	if view == "" {
		view = homeBoxViewCompact
	}
	switch view {
	case homeBoxViewCompact, homeBoxViewInfo, homeBoxViewFull:
		return view, true
	default:
		http.Error(w, "view must be compact, info, or full", http.StatusBadRequest)
		return "", false
	}
}

func shapeHomeBoxPayload(kind string, view homeBoxView, payload any) any {
	if view == homeBoxViewFull {
		return payload
	}
	switch kind {
	case "items":
		return shapeRows(extractRowsFromPayload(payload), shapeItem, view)
	case "item":
		return shapeOne(payload, shapeItem, view)
	case "tags":
		return shapeRows(extractRowsFromPayload(payload), shapeTag, view)
	case "locations":
		return shapeRows(extractRowsFromPayload(payload), shapeLocation, view)
	case "self":
		return shapeOne(payload, shapeSelf, view)
	case "statistics":
		if view == homeBoxViewCompact {
			return pickMap(unwrapMap(payload), "totalItems", "total_items", "totalLabels", "totalTags", "total_tags", "totalLocations", "total_locations", "totalValue", "total_value")
		}
		return payload
	default:
		return payload
	}
}

func shapeRows(rows []map[string]any, fn func(map[string]any, homeBoxView) map[string]any, view homeBoxView) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, fn(row, view))
	}
	return out
}

func shapeOne(payload any, fn func(map[string]any, homeBoxView) map[string]any, view homeBoxView) any {
	row := unwrapMap(payload)
	if len(row) == 0 {
		return payload
	}
	return fn(row, view)
}

func shapeItem(row map[string]any, view homeBoxView) map[string]any {
	out := map[string]any{
		"name":     firstValue(row, "name", "Name"),
		"asset_id": firstValue(row, "assetId", "asset_id", "AssetID"),
		"id":       firstValue(row, "id", "ID"),
	}
	if view == homeBoxViewInfo {
		out["description"] = firstValue(row, "description", "Description")
		out["quantity"] = firstValue(row, "quantity", "Quantity")
		out["location"] = nestedDisplay(row, "location", "Location")
		out["tags"] = nestedListDisplay(row, "tags", "labels", "Tags", "Labels")
		out["manufacturer"] = firstValue(row, "manufacturer", "Manufacturer")
		out["model_number"] = firstValue(row, "modelNumber", "model_number", "ModelNumber")
		out["serial_number"] = firstValue(row, "serialNumber", "serial_number", "SerialNumber")
		out["archived"] = firstValue(row, "archived", "Archived")
	}
	return omitEmpty(out)
}

func shapeTag(row map[string]any, view homeBoxView) map[string]any {
	out := map[string]any{
		"name": firstValue(row, "name", "Name"),
		"id":   firstValue(row, "id", "ID"),
	}
	if view == homeBoxViewInfo {
		out["description"] = firstValue(row, "description", "Description")
		out["color"] = firstValue(row, "color", "Color")
	}
	return omitEmpty(out)
}

func shapeLocation(row map[string]any, view homeBoxView) map[string]any {
	out := map[string]any{
		"name": firstValue(row, "name", "Name"),
		"id":   firstValue(row, "id", "ID"),
	}
	if view == homeBoxViewInfo {
		out["description"] = firstValue(row, "description", "Description")
		out["parent"] = nestedDisplay(row, "parent", "Parent", "location", "Location")
		out["item_count"] = firstValue(row, "itemCount", "item_count", "ItemCount")
	}
	return omitEmpty(out)
}

func shapeSelf(row map[string]any, view homeBoxView) map[string]any {
	out := map[string]any{
		"name":  firstValue(row, "name", "Name"),
		"email": firstValue(row, "email", "Email"),
		"id":    firstValue(row, "id", "ID"),
	}
	if view == homeBoxViewInfo {
		out["group_id"] = firstValue(row, "groupId", "group_id", "GroupID")
		out["role"] = firstValue(row, "role", "Role")
	}
	return omitEmpty(out)
}

func extractRowsFromPayload(payload any) []map[string]any {
	switch v := payload.(type) {
	case []any:
		return mapsFromAnyRows(v)
	case []map[string]any:
		return v
	case map[string]any:
		for _, key := range []string{"items", "Items", "results", "data", "entities", "Entities"} {
			if rows, ok := v[key].([]any); ok {
				return mapsFromAnyRows(rows)
			}
			if rows, ok := v[key].([]map[string]any); ok {
				return rows
			}
		}
		if data, ok := v["data"].(map[string]any); ok {
			if rows, ok := data["items"].([]any); ok {
				return mapsFromAnyRows(rows)
			}
		}
		if item, ok := v["item"].(map[string]any); ok {
			return []map[string]any{item}
		}
		return []map[string]any{v}
	default:
		return nil
	}
}

func mapsFromAnyRows(rows []any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if item, ok := row.(map[string]any); ok {
			out = append(out, item)
		}
	}
	return out
}

func unwrapMap(payload any) map[string]any {
	row, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"item", "data"} {
		if nested, ok := row[key].(map[string]any); ok {
			return nested
		}
	}
	return row
}

func pickMap(row map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := row[key]; ok && !emptyValue(value) {
			out[snakeKey(key)] = value
		}
	}
	if len(out) == 0 {
		return row
	}
	return out
}

func firstValue(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok && !emptyValue(value) {
			return value
		}
	}
	return nil
}

func nestedDisplay(row map[string]any, keys ...string) any {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || emptyValue(value) {
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			if display := firstValue(nested, "name", "Name"); display != nil {
				return display
			}
			if id := firstValue(nested, "id", "ID"); id != nil {
				return id
			}
		}
		return value
	}
	return nil
}

func nestedListDisplay(row map[string]any, keys ...string) []string {
	var out []string
	for _, key := range keys {
		value, ok := row[key]
		if !ok || emptyValue(value) {
			continue
		}
		rows, ok := value.([]any)
		if !ok {
			continue
		}
		for _, raw := range rows {
			if nested, ok := raw.(map[string]any); ok {
				if display := firstValue(nested, "name", "Name"); display != nil {
					out = append(out, fmt.Sprint(display))
				}
				continue
			}
			out = append(out, fmt.Sprint(raw))
		}
	}
	return out
}

func omitEmpty(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		if !emptyValue(value) {
			out[key] = value
		}
	}
	return out
}

func emptyValue(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []string:
		return len(v) == 0
	}
	return false
}

func snakeKey(key string) string {
	replacer := strings.NewReplacer("totalItems", "total_items", "totalLabels", "total_labels", "totalTags", "total_tags", "totalLocations", "total_locations", "totalValue", "total_value")
	return replacer.Replace(key)
}
