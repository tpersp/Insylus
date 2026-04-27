package homebox

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"insylus/internal/api"
	"insylus/internal/ctl"
)

func command() ctl.Command {
	return ctl.Command{
		Name:  "homebox",
		Usage: "homebox [config|set-config|remove-config|test|self|items|item|asset-template|create-asset|update-asset|tags|locations|stats] [--json]",
		Short: "Query HomeBox inventory",
		Long:  "Configure and query a HomeBox inventory server through the Insylus HomeBox plugin.",
		Examples: []string{
			"homebox set-config --base-url http://homebox:7745 --username you@example.test --password secret",
			"homebox test",
			"homebox items --query router --json",
			"homebox item --id <homebox-id> --json",
			"homebox asset-template",
			"homebox create-asset --name Router --location-id <homebox-location-id>",
			"homebox update-asset --id <homebox-id> --serial-number ABC123",
			"homebox tags --json",
			"homebox locations --json",
			"homebox stats --json",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}

func Run(args []string) {
	if len(args) == 0 {
		runItems(nil)
		return
	}
	switch args[0] {
	case "config":
		runConfig(args[1:])
	case "set-config":
		runSetConfig(args[1:])
	case "remove-config":
		runRemoveConfig(args[1:])
	case "test":
		runTest(args[1:])
	case "self":
		runGET(args[1:], "/api/homebox/self", "HomeBox self")
	case "items":
		runItems(args[1:])
	case "item":
		runItem(args[1:])
	case "asset-template", "asset-fields", "template":
		runAssetTemplate(args[1:])
	case "create-asset", "create-item":
		runCreateAsset(args[1:])
	case "update-asset", "edit-asset", "update-item", "edit-item":
		runUpdateAsset(args[1:])
	case "tags", "labels":
		runGET(args[1:], "/api/homebox/labels", "HomeBox tags")
	case "locations":
		runGET(args[1:], "/api/homebox/locations", "HomeBox locations")
	case "stats", "statistics":
		runGET(args[1:], "/api/homebox/statistics", "HomeBox statistics")
	case "--help", "-h", "help":
		PrintHelp(os.Stdout)
	default:
		api.Fatalf("unknown homebox action: %s", args[0])
	}
}

func runConfig(args []string) {
	fs := newFlagSet("homebox config")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	parseOrExit(fs, args)

	resp := mustGet(api.NewClient(*serverURL), "/api/homebox/config")
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var summary configSummary
	decode(resp.Body, &summary)
	printConfig(summary)
}

func runSetConfig(args []string) {
	fs := newFlagSet("homebox set-config")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	baseURL := fs.String("base-url", "", "HomeBox base URL without /api")
	username := fs.String("username", "", "HomeBox username or email")
	password := fs.String("password", "", "HomeBox password; defaults to HOMEBOX_PASSWORD")
	jsonOut := fs.Bool("json", false, "print JSON output")
	parseOrExit(fs, args)

	if strings.TrimSpace(*password) == "" {
		*password = os.Getenv("HOMEBOX_PASSWORD")
	}
	if strings.TrimSpace(*baseURL) == "" {
		api.Fatalf("--base-url is required")
	}
	if strings.TrimSpace(*username) == "" {
		api.Fatalf("--username is required")
	}
	if *password == "" {
		api.Fatalf("--password or HOMEBOX_PASSWORD is required")
	}
	req := configRequest{BaseURL: *baseURL, Username: *username, Password: *password}
	resp := mustPostJSON(api.NewClient(*serverURL), "/api/homebox/config", req)
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var out struct {
		Config configSummary        `json:"config"`
		Test   connectionTestResult `json:"test"`
	}
	decode(resp.Body, &out)
	fmt.Fprintf(os.Stdout, "HomeBox configured: %s as %s (%s)\n", out.Config.BaseURL, out.Config.Username, out.Test.Status)
}

func runRemoveConfig(args []string) {
	fs := newFlagSet("homebox remove-config")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	parseOrExit(fs, args)
	resp := mustPostJSON(api.NewClient(*serverURL), "/api/homebox/config/delete", map[string]string{})
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	fmt.Fprintln(os.Stdout, "HomeBox config removed")
}

func runTest(args []string) {
	fs := newFlagSet("homebox test")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	parseOrExit(fs, args)
	resp := mustPostJSON(api.NewClient(*serverURL), "/api/homebox/test", map[string]string{})
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var result connectionTestResult
	decode(resp.Body, &result)
	if result.Message != "" {
		fmt.Fprintf(os.Stdout, "%s: %s\n", result.Status, result.Message)
		return
	}
	fmt.Fprintln(os.Stdout, result.Status)
}

func runItems(args []string) {
	fs := newFlagSet("homebox items")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	query := fs.String("query", "", "HomeBox item search query")
	assetID := fs.String("asset-id", "", "HomeBox asset ID, for example 000-002")
	page := fs.Int("page", 1, "HomeBox page number")
	pageSize := fs.Int("page-size", 25, "HomeBox page size")
	jsonOut := fs.Bool("json", false, "print JSON output")
	views := addViewFlags(fs)
	parseOrExit(fs, args)
	if strings.TrimSpace(*query) != "" && strings.TrimSpace(*assetID) != "" {
		api.Fatalf("use either --query or --asset-id, not both")
	}

	path := fmt.Sprintf("/api/homebox/items?page=%d&pageSize=%d&view=%s", *page, *pageSize, views.value())
	if strings.TrimSpace(*query) != "" {
		path += "&q=" + api.URLQueryEscape(strings.TrimSpace(*query))
	}
	if strings.TrimSpace(*assetID) != "" {
		path += "&asset_id=" + api.URLQueryEscape(strings.TrimSpace(*assetID))
	}
	resp := mustGet(api.NewClient(*serverURL), path)
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var payload any
	decode(resp.Body, &payload)
	printItems(payload, views.value())
}

func runItem(args []string) {
	fs := newFlagSet("homebox item")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	id := fs.String("id", "", "HomeBox item/entity ID")
	jsonOut := fs.Bool("json", false, "print JSON output")
	views := addViewFlags(fs)
	parseOrExit(fs, args)
	if strings.TrimSpace(*id) == "" {
		api.Fatalf("--id is required")
	}
	resp := mustGet(api.NewClient(*serverURL), "/api/homebox/items/"+api.URLQueryEscape(*id)+"?view="+views.value())
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var payload any
	decode(resp.Body, &payload)
	printItems(payload, views.value())
}

func runCreateAsset(args []string) {
	fs := newFlagSet("homebox create-asset")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	views := addViewFlags(fs)
	fields := addAssetMutationFlags(fs, true)
	parseOrExit(fs, args)
	if strings.TrimSpace(*fields.name) == "" {
		api.Fatalf("--name is required")
	}
	body := assetMutationBody(fs, fields, true)
	resp := mustRequestJSON(api.NewClient(*serverURL), http.MethodPost, "/api/homebox/assets?view="+views.value(), body)
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var payload any
	decode(resp.Body, &payload)
	printItems(payload, views.value())
}

func runUpdateAsset(args []string) {
	fs := newFlagSet("homebox update-asset")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	id := fs.String("id", "", "HomeBox item/entity ID")
	jsonOut := fs.Bool("json", false, "print JSON output")
	views := addViewFlags(fs)
	fields := addAssetMutationFlags(fs, false)
	parseOrExit(fs, args)
	if strings.TrimSpace(*id) == "" {
		api.Fatalf("--id is required")
	}
	body := assetMutationBody(fs, fields, false)
	if len(body) == 0 {
		api.Fatalf("at least one editable field flag is required")
	}
	path := "/api/homebox/assets/" + api.URLQueryEscape(strings.TrimSpace(*id)) + "?view=" + views.value()
	resp := mustRequestJSON(api.NewClient(*serverURL), http.MethodPatch, path, body)
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var payload any
	decode(resp.Body, &payload)
	printItems(payload, views.value())
}

func runAssetTemplate(args []string) {
	fs := newFlagSet("homebox asset-template")
	jsonOut := fs.Bool("json", false, "print JSON output")
	parseOrExit(fs, args)
	template := assetTemplate()
	if *jsonOut {
		printJSON(template)
		return
	}
	printAssetTemplate(template)
}

func runGET(args []string, path, title string) {
	fs := newFlagSet("homebox")
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	views := addViewFlags(fs)
	parseOrExit(fs, args)
	resp := mustGet(api.NewClient(*serverURL), appendView(path, views.value()))
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var payload any
	decode(resp.Body, &payload)
	printPretty(title, payload, views.value())
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s homebox config [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox set-config --base-url URL --username USER [--password PASSWORD] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox remove-config [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox test [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox items [--query QUERY|--asset-id ASSET] [--page N] [--page-size N] [--compact|--info|--full] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox item --id ID [--compact|--info|--full] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox asset-template [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox create-asset --name NAME [--quantity N] [--asset-id ASSET] [--location-id ID] [--tag-ids IDS] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox update-asset --id ID [editable flags] [--compact|--info|--full] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox tags|locations|stats|self [--compact|--info|--full] [--json]\n\n", name)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --server        Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --json          Print JSON output\n")
	fmt.Fprintf(w, "  --compact       Compact output view for agent scans (default)\n")
	fmt.Fprintf(w, "  --info          Middle-detail output view\n")
	fmt.Fprintf(w, "  --full          Full upstream HomeBox payload\n")
	fmt.Fprintf(w, "  --password      HomeBox password; may also be supplied as HOMEBOX_PASSWORD\n")
	fmt.Fprintf(w, "Editable asset flags:\n")
	fmt.Fprintf(w, "  --name --description --quantity --asset-id --location-id --clear-location\n")
	fmt.Fprintf(w, "  --tag-ids comma-separated-ids --manufacturer --model-number --serial-number\n")
	fmt.Fprintf(w, "  --notes --purchase-from --purchase-price --purchase-date --warranty-expires\n")
}

type assetMutationFlags struct {
	name             *string
	description      *string
	quantity         *float64
	assetID          *string
	locationID       *string
	clearLocation    *bool
	entityTypeID     *string
	tagIDs           *string
	manufacturer     *string
	modelNumber      *string
	serialNumber     *string
	insured          *bool
	lifetimeWarranty *bool
	warrantyExpires  *string
	warrantyDetails  *string
	purchaseDate     *string
	purchaseFrom     *string
	purchasePrice    *float64
	notes            *string
}

func addAssetMutationFlags(fs *flag.FlagSet, create bool) assetMutationFlags {
	quantityDefault := 0.0
	if create {
		quantityDefault = 1
	}
	return assetMutationFlags{
		name:             fs.String("name", "", "asset name"),
		description:      fs.String("description", "", "asset description"),
		quantity:         fs.Float64("quantity", quantityDefault, "asset quantity"),
		assetID:          fs.String("asset-id", "", "HomeBox asset ID, for example 000-002"),
		locationID:       fs.String("location-id", "", "HomeBox location/container ID"),
		clearLocation:    fs.Bool("clear-location", false, "remove the asset location"),
		entityTypeID:     fs.String("entity-type-id", "", "HomeBox entity type ID"),
		tagIDs:           fs.String("tag-ids", "", "comma-separated HomeBox tag IDs; empty value clears tags on update"),
		manufacturer:     fs.String("manufacturer", "", "asset manufacturer"),
		modelNumber:      fs.String("model-number", "", "asset model number"),
		serialNumber:     fs.String("serial-number", "", "asset serial number"),
		insured:          fs.Bool("insured", false, "mark asset insured"),
		lifetimeWarranty: fs.Bool("lifetime-warranty", false, "mark asset as having lifetime warranty"),
		warrantyExpires:  fs.String("warranty-expires", "", "warranty expiration date, YYYY-MM-DD"),
		warrantyDetails:  fs.String("warranty-details", "", "warranty details"),
		purchaseDate:     fs.String("purchase-date", "", "purchase date, YYYY-MM-DD"),
		purchaseFrom:     fs.String("purchase-from", "", "purchase source"),
		purchasePrice:    fs.Float64("purchase-price", 0, "purchase price"),
		notes:            fs.String("notes", "", "asset notes"),
	}
}

func assetMutationBody(fs *flag.FlagSet, fields assetMutationFlags, create bool) map[string]any {
	body := map[string]any{}
	addChangedString(body, fs, "name", "name", fields.name)
	addChangedString(body, fs, "description", "description", fields.description)
	if create || flagChanged(fs, "quantity") {
		body["quantity"] = *fields.quantity
	}
	addChangedString(body, fs, "asset-id", "asset_id", fields.assetID)
	addChangedString(body, fs, "location-id", "location_id", fields.locationID)
	if flagChanged(fs, "clear-location") {
		body["clear_location"] = *fields.clearLocation
	}
	addChangedString(body, fs, "entity-type-id", "entity_type_id", fields.entityTypeID)
	if flagChanged(fs, "tag-ids") {
		body["tag_ids"] = splitCSV(*fields.tagIDs)
	}
	addChangedString(body, fs, "manufacturer", "manufacturer", fields.manufacturer)
	addChangedString(body, fs, "model-number", "model_number", fields.modelNumber)
	addChangedString(body, fs, "serial-number", "serial_number", fields.serialNumber)
	if flagChanged(fs, "insured") {
		body["insured"] = *fields.insured
	}
	if flagChanged(fs, "lifetime-warranty") {
		body["lifetime_warranty"] = *fields.lifetimeWarranty
	}
	addChangedString(body, fs, "warranty-expires", "warranty_expires", fields.warrantyExpires)
	addChangedString(body, fs, "warranty-details", "warranty_details", fields.warrantyDetails)
	addChangedString(body, fs, "purchase-date", "purchase_date", fields.purchaseDate)
	addChangedString(body, fs, "purchase-from", "purchase_from", fields.purchaseFrom)
	if flagChanged(fs, "purchase-price") {
		body["purchase_price"] = *fields.purchasePrice
	}
	addChangedString(body, fs, "notes", "notes", fields.notes)
	return body
}

func addChangedString(body map[string]any, fs *flag.FlagSet, flagName, jsonName string, value *string) {
	if flagChanged(fs, flagName) {
		body[jsonName] = *value
	}
}

func flagChanged(fs *flag.FlagSet, name string) bool {
	changed := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			changed = true
		}
	})
	return changed
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type viewFlags struct {
	compact *bool
	info    *bool
	full    *bool
	ful     *bool
}

func addViewFlags(fs *flag.FlagSet) viewFlags {
	return viewFlags{
		compact: fs.Bool("compact", false, "compact output view"),
		info:    fs.Bool("info", false, "info output view"),
		full:    fs.Bool("full", false, "full output view"),
		ful:     fs.Bool("ful", false, "alias for --full"),
	}
}

func (v viewFlags) value() string {
	count := boolCount(*v.compact, *v.info, *v.full, *v.ful)
	if count > 1 {
		api.Fatalf("choose only one of --compact, --info, or --full")
	}
	switch {
	case *v.info:
		return "info"
	case *v.full || *v.ful:
		return "full"
	default:
		return "compact"
	}
}

func appendView(path, view string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "view=" + view
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("help", false, "show help")
	fs.Bool("h", false, "show help")
	return fs
}

func parseOrExit(fs *flag.FlagSet, args []string) {
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if helpSet(fs) {
		PrintHelp(os.Stdout)
		os.Exit(0)
	}
}

func helpSet(fs *flag.FlagSet) bool {
	for _, name := range []string{"help", "h"} {
		if f := fs.Lookup(name); f != nil && f.Value.String() == "true" {
			return true
		}
	}
	return false
}

func boolCount(values ...bool) int {
	n := 0
	for _, value := range values {
		if value {
			n++
		}
	}
	return n
}

func mustGet(client api.Client, path string) *http.Response {
	resp, err := client.Get(path)
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode >= 300 {
		api.HandleErrorResponse(resp)
	}
	return resp
}

func mustPostJSON(client api.Client, path string, body any) *http.Response {
	return mustRequestJSON(client, http.MethodPost, path, body)
}

func mustRequestJSON(client api.Client, method, path string, body any) *http.Response {
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(method, client.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode >= 300 {
		api.HandleErrorResponse(resp)
	}
	return resp
}

func decode(r io.Reader, dst any) {
	if err := json.NewDecoder(r).Decode(dst); err != nil {
		api.Fatalf("decode failed: %v", err)
	}
}

func printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		api.Fatalf("encode failed: %v", err)
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func printConfig(summary configSummary) {
	if !summary.Configured {
		fmt.Fprintln(os.Stdout, "HomeBox is not configured")
		return
	}
	state := "connected"
	if !summary.Connected {
		state = "needs attention"
	}
	fmt.Fprintf(os.Stdout, "HomeBox: %s\n", state)
	fmt.Fprintf(os.Stdout, "Base URL: %s\n", summary.BaseURL)
	fmt.Fprintf(os.Stdout, "Username: %s\n", summary.Username)
	if summary.LastConnectedAt != nil {
		fmt.Fprintf(os.Stdout, "Last connected: %s\n", summary.LastConnectedAt.Format("2006-01-02 15:04:05 MST"))
	}
	if summary.TokenExpiresAt != nil {
		fmt.Fprintf(os.Stdout, "Token expires: %s\n", summary.TokenExpiresAt.Format("2006-01-02 15:04:05 MST"))
	}
	if summary.LastError != "" {
		fmt.Fprintf(os.Stdout, "Last error: %s\n", summary.LastError)
	}
}

func printAssetTemplate(template map[string]any) {
	fmt.Fprintln(os.Stdout, "HomeBox asset template")
	fmt.Fprintln(os.Stdout, "Create: insylusctl homebox create-asset --name NAME [flags]")
	fmt.Fprintln(os.Stdout, "Update: insylusctl homebox update-asset --id ID [flags]")
	fmt.Fprintln(os.Stdout, "Delete: not supported by Insylus")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "JSON body template:")
	printJSON(template["json_template"])
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Fields:")
	rows, _ := template["fields"].([]map[string]string)
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tTYPE\tCREATE\tUPDATE\tDESCRIPTION")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", row["name"], row["type"], row["create"], row["update"], row["description"])
	}
	_ = tw.Flush()
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Notes:")
	if notes, ok := template["notes"].([]string); ok {
		for _, note := range notes {
			fmt.Fprintf(os.Stdout, "  - %s\n", note)
		}
	}
}

func printItems(payload any, view string) {
	rows := extractRows(payload)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stdout, "No HomeBox items returned")
		return
	}
	if view == "full" {
		printItemDetails(rows)
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if view == "info" {
		fmt.Fprintln(tw, "NAME\tASSET ID\tLOCATION\tTAGS\tMAKE/MODEL\tSERIAL\tQTY\tID")
		for _, row := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				truncate(stringValue(row, "name", "Name"), 36),
				stringValue(row, "assetId", "asset_id", "AssetID"),
				truncate(nestedName(row, "location", "Location"), 24),
				truncate(listNames(row, "tags", "labels", "Tags", "Labels"), 24),
				truncate(makeModel(row), 28),
				stringValue(row, "serialNumber", "serial_number", "SerialNumber"),
				stringValue(row, "quantity", "Quantity"),
				stringValue(row, "id", "ID"),
			)
		}
		_ = tw.Flush()
		return
	}
	fmt.Fprintln(tw, "NAME\tASSET ID\tLOCATION\tID")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			truncate(stringValue(row, "name", "Name"), 42),
			stringValue(row, "assetId", "asset_id", "AssetID"),
			truncate(nestedName(row, "location", "Location"), 32),
			stringValue(row, "id", "ID"),
		)
	}
	_ = tw.Flush()
}

func printPretty(title string, payload any, view string) {
	switch title {
	case "HomeBox self":
		printSelf(payload, view)
	case "HomeBox tags":
		printNamedRows("TAG", payload, view)
	case "HomeBox locations":
		printNamedRows("LOCATION", payload, view)
	case "HomeBox statistics":
		printStats(payload, view)
	default:
		fmt.Fprintln(os.Stdout, title)
		printItemDetails(extractRows(payload))
	}
}

func printSelf(payload any, view string) {
	row := firstRow(payload)
	if len(row) == 0 {
		fmt.Fprintln(os.Stdout, "No HomeBox user returned")
		return
	}
	if view == "compact" {
		fmt.Fprintf(os.Stdout, "%s <%s>\n", stringValue(row, "name", "Name"), stringValue(row, "email", "Email"))
		return
	}
	fmt.Fprintln(os.Stdout, "HomeBox user")
	printDetail("Name", stringValue(row, "name", "Name"))
	printDetail("Email", stringValue(row, "email", "Email"))
	printDetail("ID", stringValue(row, "id", "ID"))
	printDetail("Role", stringValue(row, "role", "Role"))
	printDetail("Group ID", stringValue(row, "groupId", "group_id", "GroupID"))
	if view == "full" {
		printDetail("Created", stringValue(row, "createdAt", "created_at", "CreatedAt"))
		printDetail("Updated", stringValue(row, "updatedAt", "updated_at", "UpdatedAt"))
	}
}

func printNamedRows(label string, payload any, view string) {
	rows := extractRows(payload)
	if len(rows) == 0 {
		fmt.Fprintf(os.Stdout, "No HomeBox %s rows returned\n", strings.ToLower(label))
		return
	}
	if view == "full" {
		for i, row := range rows {
			if i > 0 {
				fmt.Fprintln(os.Stdout)
			}
			fmt.Fprintf(os.Stdout, "%s: %s\n", label, stringValue(row, "name", "Name"))
			printDetail("ID", stringValue(row, "id", "ID"))
			printDetail("Description", stringValue(row, "description", "Description"))
			printDetail("Color", stringValue(row, "color", "Color"))
			printDetail("Parent", nestedName(row, "parent", "Parent", "location", "Location"))
			printDetail("Item Count", stringValue(row, "itemCount", "item_count", "ItemCount"))
			printDetail("Created", stringValue(row, "createdAt", "created_at", "CreatedAt"))
			printDetail("Updated", stringValue(row, "updatedAt", "updated_at", "UpdatedAt"))
		}
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if view == "info" {
		fmt.Fprintf(tw, "%s\tDESCRIPTION\tEXTRA\tID\n", label)
		for _, row := range rows {
			extra := firstNonDash(stringValue(row, "color", "Color"), nestedName(row, "parent", "Parent", "location", "Location"), stringValue(row, "itemCount", "item_count", "ItemCount"))
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				truncate(stringValue(row, "name", "Name"), 42),
				truncate(stringValue(row, "description", "Description"), 42),
				truncate(extra, 24),
				stringValue(row, "id", "ID"),
			)
		}
		_ = tw.Flush()
		return
	}
	fmt.Fprintf(tw, "%s\tID\n", label)
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", truncate(stringValue(row, "name", "Name"), 48), stringValue(row, "id", "ID"))
	}
	_ = tw.Flush()
}

func printStats(payload any, view string) {
	row := firstRow(payload)
	if len(row) == 0 {
		fmt.Fprintln(os.Stdout, "No HomeBox statistics returned")
		return
	}
	fmt.Fprintln(os.Stdout, "HomeBox statistics")
	keys := []string{"totalItems", "total_items", "totalTags", "total_tags", "totalLabels", "total_labels", "totalLocations", "total_locations", "totalValue", "total_value"}
	if view != "compact" {
		keys = sortedKeys(row)
	}
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		printDetail(displayKey(key), fmt.Sprint(value))
	}
}

func printItemDetails(rows []map[string]any) {
	for i, row := range rows {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		fmt.Fprintf(os.Stdout, "%s\n", stringValue(row, "name", "Name"))
		printDetail("ID", stringValue(row, "id", "ID"))
		printDetail("Asset ID", stringValue(row, "assetId", "asset_id", "AssetID"))
		printDetail("Location", nestedName(row, "location", "Location"))
		printDetail("Tags", listNames(row, "tags", "labels", "Tags", "Labels"))
		printDetail("Quantity", stringValue(row, "quantity", "Quantity"))
		printDetail("Manufacturer", stringValue(row, "manufacturer", "Manufacturer"))
		printDetail("Model", stringValue(row, "modelNumber", "model_number", "ModelNumber"))
		printDetail("Serial", stringValue(row, "serialNumber", "serial_number", "SerialNumber"))
		printDetail("Purchase From", stringValue(row, "purchaseFrom", "purchase_from", "PurchaseFrom"))
		printDetail("Purchase Price", stringValue(row, "purchasePrice", "purchase_price", "PurchasePrice"))
		printDetail("Purchase Time", stringValue(row, "purchaseTime", "purchase_time", "PurchaseTime"))
		printDetail("Warranty Expires", stringValue(row, "warrantyExpires", "warranty_expires", "WarrantyExpires"))
		printDetail("Description", stringValue(row, "description", "Description"))
		printDetail("Notes", stringValue(row, "notes", "Notes"))
	}
}

func printDetail(label, value string) {
	if value == "" || value == "-" {
		return
	}
	fmt.Fprintf(os.Stdout, "  %-16s %s\n", label+":", value)
}

func extractRows(payload any) []map[string]any {
	switch v := payload.(type) {
	case []any:
		return mapsFromAny(v)
	case map[string]any:
		for _, key := range []string{"items", "Items", "results", "data", "entities", "Entities"} {
			if rows, ok := v[key].([]any); ok {
				return mapsFromAny(rows)
			}
		}
		if data, ok := v["data"].(map[string]any); ok {
			if rows, ok := data["items"].([]any); ok {
				return mapsFromAny(rows)
			}
		}
		if item, ok := v["item"].(map[string]any); ok {
			return []map[string]any{item}
		}
		return []map[string]any{v}
	}
	return nil
}

func firstRow(payload any) map[string]any {
	rows := extractRows(payload)
	if len(rows) > 0 {
		return rows[0]
	}
	if row, ok := payload.(map[string]any); ok {
		return row
	}
	return nil
}

func mapsFromAny(rows []any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if item, ok := row.(map[string]any); ok {
			out = append(out, item)
		}
	}
	return out
}

func stringValue(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	return "-"
}

func nestedName(row map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			return text
		}
		if obj, ok := value.(map[string]any); ok {
			return stringValue(obj, "name", "Name")
		}
	}
	return "-"
}

func listNames(row map[string]any, keys ...string) string {
	var names []string
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		rows, ok := value.([]any)
		if !ok {
			continue
		}
		for _, raw := range rows {
			if item, ok := raw.(map[string]any); ok {
				name := stringValue(item, "name", "Name")
				if name != "-" {
					names = append(names, name)
				}
				continue
			}
			names = append(names, fmt.Sprint(raw))
		}
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

func makeModel(row map[string]any) string {
	make := stringValue(row, "manufacturer", "Manufacturer")
	model := stringValue(row, "modelNumber", "model_number", "ModelNumber")
	if make == "-" {
		return model
	}
	if model == "-" {
		return make
	}
	return make + " " + model
}

func firstNonDash(values ...string) string {
	for _, value := range values {
		if value != "" && value != "-" {
			return value
		}
	}
	return "-"
}

func sortedKeys(row map[string]any) []string {
	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func displayKey(key string) string {
	var out []rune
	prevLower := false
	for _, ch := range key {
		if ch == '_' || ch == '-' {
			out = append(out, ' ')
			prevLower = false
			continue
		}
		if prevLower && ch >= 'A' && ch <= 'Z' {
			out = append(out, ' ')
		}
		out = append(out, ch)
		prevLower = ch >= 'a' && ch <= 'z'
	}
	if len(out) == 0 {
		return key
	}
	out[0] = []rune(strings.ToUpper(string(out[0])))[0]
	return string(out)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}
