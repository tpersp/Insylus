package wake

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"insylus/internal/api"
	"insylus/internal/ctl"
	"insylus/internal/finder"
	"insylus/internal/pluginhost"
)

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "wake"
}

func (Plugin) Name() string {
	return "Wake"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	if host.Web().Enabled() {
		rt := runtime{inventory: host.Data().Inventory()}
		host.Web().HandleFunc("POST /devices/{id}/wake", rt.handleWebWake)
	}
	if host.API().Enabled() {
		rt := runtime{inventory: host.Data().Inventory()}
		host.API().HandleFunc("POST /api/devices/{id}/wake", rt.handleAPIWake)
	}
	return nil
}

type runtime struct {
	inventory pluginhost.InventoryService
}

func (rt runtime) handleAPIWake(w http.ResponseWriter, r *http.Request) {
	device, err := rt.inventory.GetDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	result, err := WakeDevice(r.Context(), device)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrWakeOnLANUnavailable) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (rt runtime) handleWebWake(w http.ResponseWriter, r *http.Request) {
	device, err := rt.inventory.GetDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := WakeDevice(r.Context(), device); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrWakeOnLANUnavailable) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	redirect := r.Referer()
	if redirect == "" {
		redirect = "/devices/" + r.PathValue("id")
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func command() ctl.Command {
	return ctl.Command{
		Name:  "wake",
		Usage: "wake [--server URL] [--json] DEVICE",
		Short: "Send a Wake-on-LAN magic packet to a wakeable device",
		Long:  "Resolve a device and ask the server to send Wake-on-LAN when inventory says it is available.",
		Examples: []string{
			"wake MiscServer",
			"wake --json MiscServer",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}

func Run(args []string) {
	fs := flag.NewFlagSet("wake", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
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
	if fs.NArg() != 1 {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	query := fs.Arg(0)
	client := api.NewClient(*serverURL)
	item, err := finder.FindDevice(client, query)
	if err != nil {
		api.Fatalf("%v", err)
	}
	if !item.WakeOnLAN.Enabled {
		reason := item.WakeOnLAN.Reason
		if reason == "" {
			reason = "wake-on-lan is not available"
		}
		api.Fatalf("%s is not wakeable: %s", item.Identity.Name, reason)
	}
	wakeResp, err := client.Post("/api/devices/"+item.Identity.ID+"/wake", "application/json", nil)
	if err != nil {
		api.Fatalf("wake request failed: %v", err)
	}
	defer wakeResp.Body.Close()
	if wakeResp.StatusCode >= 300 {
		api.HandleErrorResponse(wakeResp)
	}
	if *jsonOut {
		api.PrintRawJSON(wakeResp.Body)
		return
	}
	var result Result
	if err := json.NewDecoder(wakeResp.Body).Decode(&result); err != nil {
		api.Fatalf("decode failed: %v", err)
	}
	if result.Status == "already_online" {
		fmt.Fprintf(os.Stdout, "%s is already online\n", result.Name)
		return
	}
	fmt.Fprintf(os.Stdout, "Wake packet sent to %s (%s) via %s\n", result.Name, result.MACAddress, strings.Join(result.Targets, ", "))
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s wake [--server URL] [--json] DEVICE\n\n", name)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --server    Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --json      Print JSON result instead of a sentence\n")
}

type Result struct {
	DeviceID   string   `json:"device_id"`
	Name       string   `json:"name"`
	MACAddress string   `json:"mac_address"`
	Targets    []string `json:"targets"`
	Status     string   `json:"status"`
	Message    string   `json:"message"`
}
