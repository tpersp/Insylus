package devices

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"insylus/internal/api"
	"insylus/internal/ctl"
	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

//go:embed templates/*.html
var templateFiles embed.FS

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "devices"
}

func (Plugin) Name() string {
	return "Devices"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(templateFiles, ".")
		if err != nil {
			return err
		}
		rt := runtime{targets: host.Targets(), inventory: host.Data().Inventory(), managed: managedProvider(host), render: host.Web().Render}
		host.Web().NavItem(pluginhost.NavItem{PluginID: "devices", Label: "Devices", Href: "/devices", Order: 10})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /devices", rt.handleTargetsPage)
		host.Web().HandleFunc("POST /devices", rt.handleCreateTarget)
		host.Web().HandleFunc("GET /devices/{id}", rt.handleTargetPage)
		host.Web().HandleFunc("POST /devices/{id}", rt.handleUpdateTarget)
		host.Web().HandleFunc("POST /devices/{id}/note", rt.handleUpdateTargetNote)
		host.Web().HandleFunc("POST /devices/{id}/delete", rt.handleDeleteTarget)
	}
	if host.API().Enabled() {
		rt := runtime{targets: host.Targets(), inventory: host.Data().Inventory(), managed: managedProvider(host)}
		host.API().HandleFunc("GET /api/devices", rt.handleTargetsAPI)
		host.API().HandleFunc("GET /api/devices/find", rt.handleTargetFindAPI)
		host.API().HandleFunc("GET /api/devices/{id}", rt.handleTargetAPI)
	}
	return nil
}

func managedProvider(host pluginhost.Host) shared.ManagedAccountConfigProvider {
	if provider, ok := host.Capabilities().Lookup("managed_account_config_provider"); ok {
		if managed, ok := provider.(shared.ManagedAccountConfigProvider); ok {
			return managed
		}
	}
	return nil
}

func command() ctl.Command {
	return ctl.Command{
		Name:  "devices",
		Usage: "devices [--server URL] [--json] [--find VALUE | --id DEVICE_ID] [--compact|--info|--full]",
		Short: "List devices or show one device",
		Long:  "List the inventory, or resolve one device by name, hostname, IP, or ID.",
		Examples: []string{
			"devices",
			"devices --json",
			"devices --find MiscServer --json",
			"devices --id cbYaSh6UZjjEnU0J --json --full",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}

func Run(args []string) {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	deviceID := fs.String("id", "", "optional device ID")
	findValue := fs.String("find", "", "find by name, hostname, IP, or ID")
	view := fs.String("view", "", "JSON output view: compact, info, or full")
	compact := fs.Bool("compact", false, "alias for --view compact")
	info := fs.Bool("info", false, "alias for --view info")
	full := fs.Bool("full", false, "alias for --view full")
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if *help || *helpShort {
		PrintHelp(os.Stdout)
		return
	}
	if *deviceID != "" && *findValue != "" {
		api.Fatalf("--find and --id are mutually exclusive")
	}
	viewChoice := strings.TrimSpace(*view)
	flagViews := 0
	if *compact {
		viewChoice = "compact"
		flagViews++
	}
	if *info {
		viewChoice = "info"
		flagViews++
	}
	if *full {
		viewChoice = "full"
		flagViews++
	}
	if flagViews > 1 {
		api.Fatalf("--compact, --info, and --full are mutually exclusive")
	}
	if viewChoice != "" && !*jsonOut {
		viewChoice = ""
	}

	path := "/api/devices"
	defaultView := "info"
	if *findValue != "" {
		path = "/api/devices/find?q=" + api.URLQueryEscape(*findValue)
	} else if *deviceID != "" {
		path = "/api/devices/" + *deviceID
	} else {
		defaultView = "compact"
		if !*jsonOut {
			defaultView = "info"
		}
	}
	if viewChoice == "" {
		viewChoice = defaultView
	}
	path = appendView(path, viewChoice)

	resp, err := http.Get(*serverURL + path)
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		api.HandleErrorResponse(resp)
	}

	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	if *findValue != "" || *deviceID != "" {
		var item shared.DeviceInventoryInfo
		if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
			var items []shared.DeviceInventoryInfo
			if err := json.NewDecoder(resp.Body).Decode(&items); err != nil || len(items) == 0 {
				api.Fatalf("decode failed: %v", err)
			}
			printInventoryInfo(items)
			return
		}
		printInventoryInfo([]shared.DeviceInventoryInfo{item})
		return
	}
	var items []shared.DeviceInventoryInfo
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		api.Fatalf("decode failed: %v", err)
	}
	printInventoryInfo(items)
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s devices [--server URL] [--json] [--find VALUE | --id DEVICE_ID] [--compact|--info|--full]\n\n", name)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --server    Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --json      Print JSON instead of a table\n")
	fmt.Fprintf(w, "  --find      Find by name, hostname, IP, or ID\n")
	fmt.Fprintf(w, "  --id        Return a single device by ID (compatibility path)\n")
	fmt.Fprintf(w, "  --compact   JSON compact view\n")
	fmt.Fprintf(w, "  --info      JSON info view\n")
	fmt.Fprintf(w, "  --full      JSON full view\n")
}

func appendView(path, view string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "view=" + api.URLQueryEscape(view)
}

func printInventoryInfo(items []shared.DeviceInventoryInfo) {
	for _, item := range items {
		host := item.Identity.Hostname
		if host == "" && len(item.Identity.IPs) > 0 {
			host = item.Identity.IPs[0]
		}
		fmt.Fprintf(os.Stdout, "%-18s %-14s %s\n", item.Identity.Name, item.Topology.Purpose, host)
	}
}
