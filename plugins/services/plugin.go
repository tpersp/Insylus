package services

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
	return "services"
}

func (Plugin) Name() string {
	return "Services"
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
		rt := runtime{
			store:     newStore(host),
			inventory: host.Data().Inventory(),
			render:    host.Web().Render,
		}
		host.Web().NavItem(pluginhost.NavItem{PluginID: "services", Label: "Services", Href: "/services", Order: 20})
		host.Web().NavItem(pluginhost.NavItem{PluginID: "services", Label: "History", Href: "/history", Order: 30})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /services", rt.handleServicesPage)
		host.Web().HandleFunc("POST /services/prune-missing", rt.handlePruneMissingServices)
		host.Web().HandleFunc("GET /history", rt.handleHistoryPage)
	}
	if host.API().Enabled() {
		rt := runtime{store: newStore(host), inventory: host.Data().Inventory()}
		host.API().HandleFunc("GET /api/services", rt.handleServices)
		host.API().HandleFunc("GET /api/services/find", rt.handleServiceFind)
	}
	return nil
}

func command() ctl.Command {
	return ctl.Command{
		Name:  "services",
		Usage: "services [--server URL] [--json] [--list] [--find VALUE] [--device VALUE] [--compact|--info|--full]",
		Short: "List or find discovered services, containers, VMs, and LXCs",
		Long:  "List the service index, search services by name or image, or show what runs on one device.",
		Examples: []string{
			"services",
			"services --json",
			"services --find jellyfin --json",
			"services --list --device docker01",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}

func Run(args []string) {
	fs := flag.NewFlagSet("services", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	list := fs.Bool("list", false, "list services")
	findValue := fs.String("find", "", "find by service name or image")
	deviceValue := fs.String("device", "", "filter services by device name, hostname, IP, or ID")
	view := fs.String("view", "", "output view: compact, info, or full")
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
	if *findValue != "" && *list {
		api.Fatalf("--find and --list are mutually exclusive")
	}
	if *findValue != "" && *deviceValue != "" {
		api.Fatalf("--find and --device are mutually exclusive")
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

	path := "/api/services"
	defaultView := "compact"
	if *findValue != "" {
		path = "/api/services/find?q=" + api.URLQueryEscape(*findValue)
		defaultView = "info"
	} else if *deviceValue != "" {
		path = "/api/services?device=" + api.URLQueryEscape(*deviceValue)
		defaultView = "info"
	}
	if viewChoice == "" {
		viewChoice = defaultView
	}
	path = api.AppendView(path, viewChoice)

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
	switch viewChoice {
	case "compact":
		var items []shared.ServiceListItem
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			api.Fatalf("decode failed: %v", err)
		}
		PrintServiceListTable(items)
	case "info":
		var items []shared.ServiceInstanceInfo
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			api.Fatalf("decode failed: %v", err)
		}
		PrintServiceInfoTable(items)
	default:
		var items []shared.ServiceInstanceFull
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			api.Fatalf("decode failed: %v", err)
		}
		PrintServiceFullTable(items)
	}
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s services [--server URL] [--json] [--list] [--find VALUE] [--device VALUE] [--compact|--info|--full]\n\n", name)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --server    Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --json      Print JSON instead of a table\n")
	fmt.Fprintf(w, "  --list      List services, grouped by name unless --device is used\n")
	fmt.Fprintf(w, "  --find      Find services by name or image\n")
	fmt.Fprintf(w, "  --device    Filter by device name, hostname, IP, or ID\n")
	fmt.Fprintf(w, "  --compact   Compact grouped service index\n")
	fmt.Fprintf(w, "  --info      Service instance info view\n")
	fmt.Fprintf(w, "  --full      Service instance full view\n")
}
