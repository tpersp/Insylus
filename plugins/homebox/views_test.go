package homebox

import "testing"

func TestShapeItemsCompactInfoFull(t *testing.T) {
	payload := map[string]any{
		"items": []any{
			map[string]any{
				"id":           "item-1",
				"name":         "Router",
				"assetId":      float64(42),
				"description":  "Edge router",
				"manufacturer": "MikroTik",
				"location":     map[string]any{"name": "Rack"},
				"tags":         []any{map[string]any{"name": "network"}},
			},
		},
		"total": float64(1),
	}
	compact := shapeHomeBoxPayload("items", homeBoxViewCompact, payload).([]map[string]any)
	if len(compact) != 1 || compact[0]["name"] != "Router" || compact[0]["description"] != nil {
		t.Fatalf("unexpected compact payload: %#v", compact)
	}
	info := shapeHomeBoxPayload("items", homeBoxViewInfo, payload).([]map[string]any)
	if info[0]["description"] != "Edge router" || info[0]["location"] != "Rack" {
		t.Fatalf("unexpected info payload: %#v", info)
	}
	if full := shapeHomeBoxPayload("items", homeBoxViewFull, payload); len(full.(map[string]any)) != len(payload) {
		t.Fatalf("full payload was not preserved: %#v", full)
	}
}

func TestShapeTagsAndLocations(t *testing.T) {
	tagPayload := []any{map[string]any{"id": "tag-1", "name": "network", "color": "#00f"}}
	tags := shapeHomeBoxPayload("tags", homeBoxViewInfo, tagPayload).([]map[string]any)
	if tags[0]["name"] != "network" || tags[0]["color"] != "#00f" {
		t.Fatalf("unexpected tags payload: %#v", tags)
	}
	locationPayload := map[string]any{"items": []any{map[string]any{"id": "loc-1", "name": "Rack"}}}
	locations := shapeHomeBoxPayload("locations", homeBoxViewCompact, locationPayload).([]map[string]any)
	if locations[0]["name"] != "Rack" || locations[0]["id"] != "loc-1" {
		t.Fatalf("unexpected locations payload: %#v", locations)
	}
}
