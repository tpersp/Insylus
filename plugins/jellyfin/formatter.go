package jellyfin

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

func Run(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "set-token":
			runSetToken(args[1:])
			return
		case "list-tokens":
			runListTokens(args[1:])
			return
		case "remove-token":
			runRemoveToken(args[1:])
			return
		}
	}
	runOperation(args)
}

func runOperation(args []string) {
	fs := flag.NewFlagSet("jellyfin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	device := fs.String("device", "", "Jellyfin server device name, hostname, IP, or ID")
	jsonOut := fs.Bool("json", false, "print JSON output")
	list := fs.Bool("list", false, "list all items (movies and series)")
	movies := fs.Bool("movies", false, "list movies")
	series := fs.Bool("series", false, "list TV series")
	episodes := fs.Bool("episodes", false, "list episodes")
	latest := fs.Bool("latest", false, "show recently added items")
	resume := fs.Bool("resume", false, "show in-progress items")
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
	if strings.TrimSpace(*device) == "" {
		api.Fatalf("--device is required")
	}
	actionCount := boolCount(*list, *movies, *series, *episodes, *latest, *resume)
	if actionCount == 0 {
		*list = true
	} else if actionCount > 1 {
		api.Fatalf("choose exactly one Jellyfin action")
	}
	client := api.NewClient(*serverURL)
	deviceInfo := resolveDevice(client, *device)
	deviceID := deviceInfo.DeviceID
	switch {
	case *list:
		moviesResp := mustGet(client, "/api/jellyfin/"+deviceID+"/movies")
		defer moviesResp.Body.Close()
		var moviesResult []JellyfinItem
		decode(moviesResp.Body, &moviesResult)

		seriesResp := mustGet(client, "/api/jellyfin/"+deviceID+"/series")
		defer seriesResp.Body.Close()
		var seriesResult []JellyfinItem
		decode(seriesResp.Body, &seriesResult)

		if *jsonOut {
			allItems := append(moviesResult, seriesResult...)
			sortItemsByName(allItems)
			api.PrintRawJSON(bytes.NewReader(mustMarshal(allItems)))
			return
		}
		printMoviesAndSeries(moviesResult, seriesResult)
	case *movies:
		resp := mustGet(client, "/api/jellyfin/"+deviceID+"/movies")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var items []JellyfinItem
		decode(resp.Body, &items)
		printMovies([]JellyfinItem{}, items)
	case *series:
		resp := mustGet(client, "/api/jellyfin/"+deviceID+"/series")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var items []JellyfinItem
		decode(resp.Body, &items)
		printSeries(items)
	case *episodes:
		resp := mustGet(client, "/api/jellyfin/"+deviceID+"/episodes")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var items []JellyfinItem
		decode(resp.Body, &items)
		printEpisodes(items)
	case *latest:
		resp := mustGet(client, "/api/jellyfin/"+deviceID+"/latest")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var items []JellyfinItem
		decode(resp.Body, &items)
		printLatest(items)
	case *resume:
		resp := mustGet(client, "/api/jellyfin/"+deviceID+"/resume")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var items []ItemProgress
		decode(resp.Body, &items)
		printResume(items)
	}
}

func runSetToken(args []string) {
	fs := flag.NewFlagSet("jellyfin set-token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	device := fs.String("device", "", "Insylus device or existing Jellyfin server name")
	deviceID := fs.String("device-id", "", "Insylus device ID")
	serverName := fs.String("server-name", "", "Jellyfin server display name")
	apiURL := fs.String("api-url", "", "Jellyfin server URL, default http://device:8096")
	apiKey := fs.String("api-key", "", "Jellyfin API key")
	defaultUserID := fs.String("user-id", "", "Jellyfin user ID for library queries")
	defaultUsername := fs.String("username", "", "Jellyfin username (for reference)")
	tlsInsecure := fs.Bool("tls-insecure", false, "allow self-signed certificate")
	jsonOut := fs.Bool("json", false, "print JSON output")
	help := fs.Bool("help", false, "show help")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if *help {
		PrintHelp(os.Stdout)
		return
	}
	if *deviceID == "" && *device == "" {
		api.Fatalf("--device or --device-id is required")
	}
	if *apiKey == "" {
		api.Fatalf("--api-key is required")
	}
	req := map[string]any{
		"device_id":        *deviceID,
		"server":           *device,
		"server_name":      *serverName,
		"api_url":          *apiURL,
		"api_key":          *apiKey,
		"default_user_id":  *defaultUserID,
		"default_username": *defaultUsername,
		"tls_insecure":     *tlsInsecure,
	}
	resp := mustPostJSON(api.NewClient(*serverURL), "/api/jellyfin/tokens", req)
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var summary jellyfinTokenSummary
	decode(resp.Body, &summary)
	fmt.Fprintf(os.Stdout, "Stored Jellyfin token for %s (%s)\n", summary.DeviceName, summary.ServerName)
}

func runListTokens(args []string) {
	fs := flag.NewFlagSet("jellyfin list-tokens", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	resp := mustGet(api.NewClient(*serverURL), "/api/jellyfin/servers")
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var servers []jellyfinTokenSummary
	decode(resp.Body, &servers)
	printTokenSummaries(servers)
}

func runRemoveToken(args []string) {
	fs := flag.NewFlagSet("jellyfin remove-token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	device := fs.String("device", "", "Jellyfin server device name, hostname, IP, or ID")
	deviceID := fs.String("device-id", "", "Insylus device ID")
	jsonOut := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	client := api.NewClient(*serverURL)
	id := strings.TrimSpace(*deviceID)
	if id == "" {
		if *device == "" {
			api.Fatalf("--device or --device-id is required")
		}
		id = resolveDevice(client, *device).DeviceID
	}
	resp := mustPostJSON(client, "/api/jellyfin/tokens/delete/"+id, map[string]string{"device_id": id})
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	fmt.Fprintln(os.Stdout, "Removed Jellyfin token")
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s jellyfin --device DEVICE [--list|--movies|--series|--episodes|--latest|--resume] [--json]\n", name)
	fmt.Fprintf(w, "  %s jellyfin set-token --device DEVICE --api-key KEY [--server-name NAME] [--api-url URL] [--tls-insecure]\n", name)
	fmt.Fprintf(w, "  %s jellyfin list-tokens [--json]\n", name)
	fmt.Fprintf(w, "  %s jellyfin remove-token --device DEVICE\n\n", name)
	fmt.Fprintf(w, "Token setup:\n")
	fmt.Fprintf(w, "  Create an API key in Jellyfin Dashboard > API Keys first. Insylus never creates Jellyfin API keys.\n\n")
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --server        Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --device        Jellyfin server device name, hostname, IP, or ID\n")
	fmt.Fprintf(w, "  --json          Print JSON output\n")
}

func resolveDevice(client api.Client, query string) jellyfinTokenSummary {
	var servers []jellyfinTokenSummary
	if err := client.DecodeGET("/api/jellyfin/servers", &servers); err != nil {
		api.Fatalf("%v", err)
	}
	var matches []jellyfinTokenSummary
	for _, s := range servers {
		if strings.EqualFold(s.DeviceID, query) ||
			strings.EqualFold(s.DeviceName, query) ||
			strings.EqualFold(s.Hostname, query) ||
			strings.EqualFold(s.ServerName, query) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		api.Fatalf("no Jellyfin server named %q is enrolled or configured", query)
	}
	if len(matches) > 1 {
		api.Fatalf("multiple Jellyfin servers match %q; use device ID", query)
	}
	if !matches[0].HasToken {
		api.Fatalf("no Jellyfin API key configured for %s\nHint: create an API key in Jellyfin, then run: %s jellyfin set-token --device %s --api-key \"your-api-key\"", matches[0].DeviceName, ctl.CommandName(), query)
	}
	return matches[0]
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

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		api.Fatalf("marshal failed: %v", err)
	}
	return data
}

func decode(r io.Reader, dst any) {
	if err := json.NewDecoder(r).Decode(dst); err != nil {
		api.Fatalf("decode failed: %v", err)
	}
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

func printMoviesAndSeries(movies, series []JellyfinItem) {
	tw := newTabwriter()
	fmt.Fprintln(tw, "TYPE\tNAME\tRUNTIME\tWATCHED\tPROGRESS")
	for _, item := range movies {
		watched, progress := getWatchedInfo(item)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			ItemTypeDisplay(item.Type),
			truncate(item.Name, 40),
			FormatRuntime(item.RunTimeTicks),
			watched,
			progress)
	}
	for _, item := range series {
		watched, _ := getWatchedInfo(item)
		episodeInfo := ""
		if item.ChildCount > 0 {
			episodeInfo = fmt.Sprintf(" (%d episodes)", item.ChildCount)
		}
		fmt.Fprintf(tw, "%s\t%s%s\t%s\t%s\t%s\n",
			ItemTypeDisplay(item.Type),
			truncate(item.Name, 40),
			episodeInfo,
			FormatRuntime(item.RunTimeTicks),
			watched,
			"-")
	}
	_ = tw.Flush()
}

func printMovies(movies, series []JellyfinItem) {
	tw := newTabwriter()
	fmt.Fprintln(tw, "TYPE\tNAME\tRUNTIME\tWATCHED\tPROGRESS")
	for _, item := range movies {
		watched, progress := getWatchedInfo(item)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			ItemTypeDisplay(item.Type),
			truncate(item.Name, 40),
			FormatRuntime(item.RunTimeTicks),
			watched,
			progress)
	}
	_ = tw.Flush()
}

func printSeries(items []JellyfinItem) {
	tw := newTabwriter()
	fmt.Fprintln(tw, "SERIES\tEPISODES\tPREMIERED\tWATCHED")
	for _, item := range items {
		watched, _ := getWatchedInfo(item)
		premiered := ""
		if !item.PremiereDate.IsZero() {
			premiered = item.PremiereDate.Format("2006")
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n",
			truncate(item.Name, 40),
			item.ChildCount,
			premiered,
			watched)
	}
	_ = tw.Flush()
}

func printEpisodes(items []JellyfinItem) {
	tw := newTabwriter()
	fmt.Fprintln(tw, "SERIES\tEPISODE\tNAME\tRUNTIME\tWATCHED\tPROGRESS")
	for _, item := range items {
		watched, progress := getWatchedInfo(item)
		episodeLabel := fmt.Sprintf("S%d E%d", item.SeasonNumber, item.EpisodeNumber)
		if item.SpecialEpisodeNumber > 0 {
			episodeLabel = fmt.Sprintf("S%d SP%d", item.SeasonNumber, item.SpecialEpisodeNumber)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			truncate(item.SeriesName, 25),
			episodeLabel,
			truncate(item.Name, 30),
			FormatRuntime(item.RunTimeTicks),
			watched,
			progress)
	}
	_ = tw.Flush()
}

func printLatest(items []JellyfinItem) {
	tw := newTabwriter()
	fmt.Fprintln(tw, "TYPE\tNAME\tDATE")
	sort.Slice(items, func(i, j int) bool {
		return items[i].PremiereDate.After(items[j].PremiereDate)
	})
	for _, item := range items {
		date := ""
		if !item.PremiereDate.IsZero() {
			date = item.PremiereDate.Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			ItemTypeDisplay(item.Type),
			truncate(item.Name, 50),
			date)
	}
	_ = tw.Flush()
}

func printResume(items []ItemProgress) {
	tw := newTabwriter()
	fmt.Fprintln(tw, "TYPE\tSERIES\tNAME\tPOSITION\tDURATION\tPROGRESS")
	for _, item := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%.0f%%\n",
			ItemTypeDisplay(item.Type),
			truncate(item.SeriesName, 25),
			truncate(item.Name, 30),
			item.Position,
			item.Duration,
			item.Progress)
	}
	_ = tw.Flush()
}

func printTokenSummaries(items []jellyfinTokenSummary) {
	tw := newTabwriter()
	fmt.Fprintln(tw, "DEVICE\tSERVER\tAPI URL\tTLS")
	for _, item := range items {
		tlsMode := "verify"
		if item.TLSInsecure {
			tlsMode = "insecure"
		}
		apiURL := item.APIURL
		if apiURL == "" {
			apiURL = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.DeviceName, item.ServerName, apiURL, tlsMode)
	}
	_ = tw.Flush()
}

func newTabwriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

func getWatchedInfo(item JellyfinItem) (watched string, progress string) {
	if item.UserData == nil {
		return "-", "-"
	}
	if item.UserData.Played {
		return "Yes", "100%"
	}
	if item.UserData.PlaybackPositionTicks > 0 {
		pct := progressPercent(item.UserData.PlaybackPositionTicks, item.RunTimeTicks)
		return "No", fmt.Sprintf("%.0f%%", pct)
	}
	return "No", "-"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}
