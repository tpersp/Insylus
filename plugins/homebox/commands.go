package homebox

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"insylus/internal/api"
	"insylus/internal/ctl"
)

func command() ctl.Command {
	return ctl.Command{
		Name:  "homebox",
		Usage: "homebox [config|set-config|remove-config|test|self|items|item|tags|locations|stats] [--json]",
		Short: "Query HomeBox inventory",
		Long:  "Configure and query a HomeBox inventory server through the Insylus HomeBox plugin.",
		Examples: []string{
			"homebox set-config --base-url http://homebox:7745 --username you@example.test --password secret",
			"homebox test",
			"homebox items --query router --json",
			"homebox item --id <homebox-id> --json",
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
	page := fs.Int("page", 1, "HomeBox page number")
	pageSize := fs.Int("page-size", 25, "HomeBox page size")
	jsonOut := fs.Bool("json", false, "print JSON output")
	views := addViewFlags(fs)
	parseOrExit(fs, args)

	path := fmt.Sprintf("/api/homebox/items?page=%d&pageSize=%d&view=%s", *page, *pageSize, views.value())
	if strings.TrimSpace(*query) != "" {
		path += "&q=" + api.URLQueryEscape(strings.TrimSpace(*query))
	}
	resp := mustGet(api.NewClient(*serverURL), path)
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var payload any
	decode(resp.Body, &payload)
	printItems(payload)
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
	api.PrintRawJSON(resp.Body)
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
	fmt.Fprintln(os.Stdout, title)
	api.PrintRawJSON(resp.Body)
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s homebox config [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox set-config --base-url URL --username USER [--password PASSWORD] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox remove-config [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox test [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox items [--query QUERY] [--page N] [--page-size N] [--compact|--info|--full] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox item --id ID [--compact|--info|--full] [--json]\n", name)
	fmt.Fprintf(w, "  %s homebox tags|locations|stats|self [--compact|--info|--full] [--json]\n\n", name)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --server        Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --json          Print JSON output\n")
	fmt.Fprintf(w, "  --compact       Compact output view for agent scans (default)\n")
	fmt.Fprintf(w, "  --info          Middle-detail output view\n")
	fmt.Fprintf(w, "  --full          Full upstream HomeBox payload\n")
	fmt.Fprintf(w, "  --password      HomeBox password; may also be supplied as HOMEBOX_PASSWORD\n")
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
	data, _ := json.Marshal(body)
	resp, err := client.Post(path, "application/json", bytes.NewReader(data))
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

func printItems(payload any) {
	rows := extractRows(payload)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stdout, "No HomeBox items returned")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
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

func extractRows(payload any) []map[string]any {
	switch v := payload.(type) {
	case []any:
		return mapsFromAny(v)
	case map[string]any:
		for _, key := range []string{"items", "Items", "results", "data"} {
			if rows, ok := v[key].([]any); ok {
				return mapsFromAny(rows)
			}
		}
		if data, ok := v["data"].(map[string]any); ok {
			if rows, ok := data["items"].([]any); ok {
				return mapsFromAny(rows)
			}
		}
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}
