package homebox

import "testing"

func TestMergedEntityUpdatePreservesCurrentFields(t *testing.T) {
	current := map[string]any{
		"id":          "item-1",
		"assetId":     "000-001",
		"name":        "Old name",
		"description": "Old description",
		"quantity":    float64(2),
		"parent": map[string]any{
			"id":   "location-1",
			"name": "Office",
		},
		"entityType": map[string]any{
			"id":   "type-1",
			"name": "Item",
		},
		"tags": []any{
			map[string]any{"id": "tag-1", "name": "Hardware"},
		},
		"manufacturer": "Old maker",
	}
	name := "New name"
	serial := "ABC123"

	got := mergedEntityUpdate(current, assetMutationRequest{
		Name:         &name,
		SerialNumber: &serial,
	})

	if got["name"] != "New name" {
		t.Fatalf("name = %v, want New name", got["name"])
	}
	if got["description"] != "Old description" {
		t.Fatalf("description = %v, want preserved old description", got["description"])
	}
	if got["manufacturer"] != "Old maker" {
		t.Fatalf("manufacturer = %v, want preserved old maker", got["manufacturer"])
	}
	if got["serialNumber"] != "ABC123" {
		t.Fatalf("serialNumber = %v, want ABC123", got["serialNumber"])
	}
	if got["parentId"] != "location-1" {
		t.Fatalf("parentId = %v, want location-1", got["parentId"])
	}
	tagIDs, ok := got["tagIds"].([]string)
	if !ok || len(tagIDs) != 1 || tagIDs[0] != "tag-1" {
		t.Fatalf("tagIds = %#v, want [tag-1]", got["tagIds"])
	}
}

func TestMergedEntityUpdateCanClearLocationAndTags(t *testing.T) {
	current := map[string]any{
		"id":       "item-1",
		"name":     "Item",
		"quantity": float64(1),
		"parent":   map[string]any{"id": "location-1"},
		"tags":     []any{map[string]any{"id": "tag-1"}},
	}

	got := mergedEntityUpdate(current, assetMutationRequest{
		ClearLocation: true,
		TagIDs:        []string{},
	})

	if got["parentId"] != nil {
		t.Fatalf("parentId = %v, want nil", got["parentId"])
	}
	tagIDs, ok := got["tagIds"].([]string)
	if !ok || len(tagIDs) != 0 {
		t.Fatalf("tagIds = %#v, want empty []string", got["tagIds"])
	}
}
