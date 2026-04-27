package homebox

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"insylus/internal/httpx"
)

type assetMutationRequest struct {
	Name             *string  `json:"name,omitempty"`
	Description      *string  `json:"description,omitempty"`
	Quantity         *float64 `json:"quantity,omitempty"`
	AssetID          *string  `json:"asset_id,omitempty"`
	LocationID       *string  `json:"location_id,omitempty"`
	ParentID         *string  `json:"parent_id,omitempty"`
	ClearLocation    bool     `json:"clear_location,omitempty"`
	EntityTypeID     *string  `json:"entity_type_id,omitempty"`
	TagIDs           []string `json:"tag_ids,omitempty"`
	Manufacturer     *string  `json:"manufacturer,omitempty"`
	ModelNumber      *string  `json:"model_number,omitempty"`
	SerialNumber     *string  `json:"serial_number,omitempty"`
	Insured          *bool    `json:"insured,omitempty"`
	LifetimeWarranty *bool    `json:"lifetime_warranty,omitempty"`
	WarrantyExpires  *string  `json:"warranty_expires,omitempty"`
	WarrantyDetails  *string  `json:"warranty_details,omitempty"`
	PurchaseDate     *string  `json:"purchase_date,omitempty"`
	PurchaseFrom     *string  `json:"purchase_from,omitempty"`
	PurchasePrice    *float64 `json:"purchase_price,omitempty"`
	Notes            *string  `json:"notes,omitempty"`
}

func (rt runtime) handleAssetTemplate(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, assetTemplate())
}

func (rt runtime) handleCreateAsset(w http.ResponseWriter, r *http.Request) {
	var req assetMutationRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	client, err := rt.client(r.Context())
	if err != nil {
		http.Error(w, classifyError(err), http.StatusBadRequest)
		return
	}

	body := map[string]any{
		"name":     strings.TrimSpace(*req.Name),
		"quantity": 1,
	}
	if req.Quantity != nil {
		body["quantity"] = *req.Quantity
	}
	if req.Description != nil {
		body["description"] = *req.Description
	}
	if parentID, ok := req.location(); ok {
		body["parentId"] = parentID
	}
	if req.EntityTypeID != nil {
		body["entityTypeId"] = strings.TrimSpace(*req.EntityTypeID)
	}
	if req.TagIDs != nil {
		body["tagIds"] = req.TagIDs
	}

	var out any
	if err := client.RequestJSON(r.Context(), http.MethodPost, "/v1/entities", body, &out); err != nil {
		rt.writeHomeBoxError(w, r, err)
		return
	}
	_ = rt.store.markConnected(r.Context())

	row := firstRow(out)
	id := stringValue(row, "id", "ID")
	if id != "-" && req.needsFullUpdateAfterCreate() {
		updated, err := rt.applyAssetUpdate(r, client, id, req)
		if err != nil {
			rt.writeHomeBoxError(w, r, err)
			return
		}
		out = updated
	}
	rt.writeHomeBoxJSON(w, r, "item", out)
}

func assetTemplate() map[string]any {
	fields := []map[string]string{
		{"name": "name", "type": "string", "create": "required", "update": "optional", "description": "Asset name."},
		{"name": "description", "type": "string", "create": "optional", "update": "optional", "description": "Short asset description."},
		{"name": "quantity", "type": "number", "create": "optional default 1", "update": "optional", "description": "Asset quantity."},
		{"name": "asset_id", "type": "string", "create": "optional", "update": "optional", "description": "Formatted HomeBox asset ID, for example 000-012."},
		{"name": "location_id", "type": "string", "create": "optional", "update": "optional", "description": "HomeBox location/container entity ID."},
		{"name": "clear_location", "type": "boolean", "create": "ignored", "update": "optional", "description": "Set true to remove the asset from its current location."},
		{"name": "entity_type_id", "type": "string", "create": "optional", "update": "optional", "description": "HomeBox entity type ID. Omit for the default item type."},
		{"name": "tag_ids", "type": "array<string>", "create": "optional", "update": "optional", "description": "HomeBox tag IDs. On update, an empty array clears tags."},
		{"name": "manufacturer", "type": "string", "create": "optional", "update": "optional", "description": "Manufacturer or brand."},
		{"name": "model_number", "type": "string", "create": "optional", "update": "optional", "description": "Model number."},
		{"name": "serial_number", "type": "string", "create": "optional", "update": "optional", "description": "Serial number."},
		{"name": "insured", "type": "boolean", "create": "optional", "update": "optional", "description": "Whether the asset is insured."},
		{"name": "lifetime_warranty", "type": "boolean", "create": "optional", "update": "optional", "description": "Whether the asset has a lifetime warranty."},
		{"name": "warranty_expires", "type": "string", "create": "optional", "update": "optional", "description": "Warranty expiration date, YYYY-MM-DD."},
		{"name": "warranty_details", "type": "string", "create": "optional", "update": "optional", "description": "Warranty details."},
		{"name": "purchase_date", "type": "string", "create": "optional", "update": "optional", "description": "Purchase date, YYYY-MM-DD."},
		{"name": "purchase_from", "type": "string", "create": "optional", "update": "optional", "description": "Vendor or source."},
		{"name": "purchase_price", "type": "number", "create": "optional", "update": "optional", "description": "Purchase price."},
		{"name": "notes", "type": "string", "create": "optional", "update": "optional", "description": "Longer operator notes."},
	}
	return map[string]any{
		"resource":        "homebox_asset",
		"non_destructive": true,
		"delete":          "not supported by Insylus",
		"create_endpoint": "POST /api/homebox/assets?view=compact|info|full",
		"update_endpoint": "PATCH /api/homebox/assets/{id}?view=compact|info|full",
		"create_command":  "insylusctl homebox create-asset --name NAME [flags]",
		"update_command":  "insylusctl homebox update-asset --id ID [flags]",
		"notes": []string{
			"Create requires name. Quantity defaults to 1 when omitted.",
			"Update merges only supplied fields with the current HomeBox item so omitted fields are preserved.",
			"Use insylusctl homebox locations to discover location_id values.",
			"Use insylusctl homebox tags to discover tag_ids values.",
		},
		"json_template": map[string]any{
			"name":              "Asset name",
			"description":       "Short description",
			"quantity":          1,
			"asset_id":          "000-012",
			"location_id":       "homebox-location-uuid",
			"clear_location":    false,
			"entity_type_id":    "homebox-entity-type-uuid",
			"tag_ids":           []string{"homebox-tag-uuid"},
			"manufacturer":      "Manufacturer",
			"model_number":      "Model",
			"serial_number":     "Serial",
			"insured":           false,
			"lifetime_warranty": false,
			"warranty_expires":  "2027-12-31",
			"warranty_details":  "Warranty details",
			"purchase_date":     "2026-04-27",
			"purchase_from":     "Vendor",
			"purchase_price":    0,
			"notes":             "Notes",
		},
		"fields": fields,
	}
}

func (rt runtime) handleUpdateAsset(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	var req assetMutationRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	client, err := rt.client(r.Context())
	if err != nil {
		http.Error(w, classifyError(err), http.StatusBadRequest)
		return
	}
	out, err := rt.applyAssetUpdate(r, client, id, req)
	if err != nil {
		rt.writeHomeBoxError(w, r, err)
		return
	}
	rt.writeHomeBoxJSON(w, r, "item", out)
}

func (rt runtime) applyAssetUpdate(r *http.Request, client *Client, id string, req assetMutationRequest) (any, error) {
	var current any
	if err := client.GetJSON(r.Context(), "/v1/entities/"+url.PathEscape(id), &current); err != nil {
		return nil, err
	}
	currentRow := firstRow(current)
	if len(currentRow) == 0 {
		return nil, fmt.Errorf("Unexpected API response: HomeBox item %s was empty", id)
	}
	body := mergedEntityUpdate(currentRow, req)
	var out any
	if err := client.RequestJSON(r.Context(), http.MethodPut, "/v1/entities/"+url.PathEscape(id), body, &out); err != nil {
		return nil, err
	}
	_ = rt.store.markConnected(r.Context())
	return out, nil
}

func mergedEntityUpdate(current map[string]any, req assetMutationRequest) map[string]any {
	body := map[string]any{}
	copyKeys(body, current,
		"id", "assetId", "name", "description", "quantity", "insured", "archived",
		"syncChildEntityLocations", "serialNumber", "modelNumber", "manufacturer",
		"lifetimeWarranty", "warrantyExpires", "warrantyDetails", "purchaseDate",
		"purchaseFrom", "purchasePrice", "soldDate", "soldTo", "soldPrice", "soldNotes",
		"notes", "fields",
	)
	body["parentId"] = nestedID(current, "parent", "Parent")
	body["entityTypeId"] = nestedID(current, "entityType", "EntityType")
	body["tagIds"] = nestedIDs(current, "tags", "Tags")

	setString(body, "name", req.Name)
	setString(body, "description", req.Description)
	setFloat(body, "quantity", req.Quantity)
	setString(body, "assetId", req.AssetID)
	if req.ClearLocation {
		body["parentId"] = nil
	} else if parentID, ok := req.location(); ok {
		body["parentId"] = parentID
	}
	setString(body, "entityTypeId", req.EntityTypeID)
	if req.TagIDs != nil {
		body["tagIds"] = req.TagIDs
	}
	setString(body, "manufacturer", req.Manufacturer)
	setString(body, "modelNumber", req.ModelNumber)
	setString(body, "serialNumber", req.SerialNumber)
	setBool(body, "insured", req.Insured)
	setBool(body, "lifetimeWarranty", req.LifetimeWarranty)
	setString(body, "warrantyExpires", req.WarrantyExpires)
	setString(body, "warrantyDetails", req.WarrantyDetails)
	setString(body, "purchaseDate", req.PurchaseDate)
	setString(body, "purchaseFrom", req.PurchaseFrom)
	setFloat(body, "purchasePrice", req.PurchasePrice)
	setString(body, "notes", req.Notes)
	return body
}

func (req assetMutationRequest) location() (string, bool) {
	if req.LocationID != nil {
		return strings.TrimSpace(*req.LocationID), strings.TrimSpace(*req.LocationID) != ""
	}
	if req.ParentID != nil {
		return strings.TrimSpace(*req.ParentID), strings.TrimSpace(*req.ParentID) != ""
	}
	return "", false
}

func (req assetMutationRequest) needsFullUpdateAfterCreate() bool {
	return req.AssetID != nil ||
		req.Manufacturer != nil ||
		req.ModelNumber != nil ||
		req.SerialNumber != nil ||
		req.Insured != nil ||
		req.LifetimeWarranty != nil ||
		req.WarrantyExpires != nil ||
		req.WarrantyDetails != nil ||
		req.PurchaseDate != nil ||
		req.PurchaseFrom != nil ||
		req.PurchasePrice != nil ||
		req.Notes != nil
}

func copyKeys(dst, src map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func setString(dst map[string]any, key string, value *string) {
	if value != nil {
		dst[key] = *value
	}
}

func setFloat(dst map[string]any, key string, value *float64) {
	if value != nil {
		dst[key] = *value
	}
}

func setBool(dst map[string]any, key string, value *bool) {
	if value != nil {
		dst[key] = *value
	}
}

func nestedID(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if obj, ok := row[key].(map[string]any); ok {
			if id := stringValue(obj, "id", "ID"); id != "-" {
				return id
			}
		}
	}
	return nil
}

func nestedIDs(row map[string]any, keys ...string) []string {
	ids := []string{}
	for _, key := range keys {
		rows, ok := row[key].([]any)
		if !ok {
			continue
		}
		for _, raw := range rows {
			if obj, ok := raw.(map[string]any); ok {
				if id := stringValue(obj, "id", "ID"); id != "-" {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}
