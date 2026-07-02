package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"insylus/internal/httpapi"
	"insylus/internal/model"
	"insylus/internal/store"
)

func Run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "server":
		return server(args[1:])
	case "principal":
		return principal(args[1:])
	case "access":
		return access(args[1:])
	case "help", "-h", "--help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dbPath := dbFlag(fs)
	listen := fs.String("listen", ":8097", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	fmt.Printf("Insylus listening on %s\n", listenURL(*listen))
	return http.ListenAndServe(*listen, httpapi.New(st))
}

func server(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("server requires add, update, delete, or list")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("server add", flag.ContinueOnError)
		target := targetFlags(fs)
		name := fs.String("name", "", "server name")
		host := fs.String("host", "", "hostname")
		addr := fs.String("addr", "", "address")
		notes := fs.String("notes", "", "notes")
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		in := model.Server{Name: *name, Hostname: *host, Address: *addr, Notes: *notes}
		if target.useAPI() {
			var item model.Server
			if err := postAPI(target.apiURL, "/api/servers", in, &item); err != nil {
				return err
			}
			return printValue(item, *jsonOut)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			item, err := st.CreateServer(context.Background(), in)
			if err != nil {
				return err
			}
			return printValue(item, *jsonOut)
		})
	case "list":
		fs := flag.NewFlagSet("server list", flag.ContinueOnError)
		target := targetFlags(fs)
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if target.useAPI() {
			var items []model.Server
			if err := getAPI(target.apiURL, "/api/servers", &items); err != nil {
				return err
			}
			return printServers(items, *jsonOut)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			items, err := st.ListServers(context.Background())
			if err != nil {
				return err
			}
			return printServers(items, *jsonOut)
		})
	case "update":
		fs := flag.NewFlagSet("server update", flag.ContinueOnError)
		target := targetFlags(fs)
		id := fs.Int64("id", 0, "server id")
		name := fs.String("name", "", "server name")
		host := fs.String("host", "", "hostname")
		addr := fs.String("addr", "", "address")
		notes := fs.String("notes", "", "notes")
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		in := model.Server{ID: *id, Name: *name, Hostname: *host, Address: *addr, Notes: *notes}
		if target.useAPI() {
			var item model.Server
			if err := putAPI(target.apiURL, "/api/servers", in, &item); err != nil {
				return err
			}
			return printUpdated("server", item.Name, *jsonOut, item)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			item, err := st.UpdateServer(context.Background(), in)
			if err != nil {
				return err
			}
			return printUpdated("server", item.Name, *jsonOut, item)
		})
	case "delete":
		fs := flag.NewFlagSet("server delete", flag.ContinueOnError)
		target := targetFlags(fs)
		id := fs.Int64("id", 0, "server id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if target.useAPI() {
			if err := deleteAPI(target.apiURL, "/api/servers", *id); err != nil {
				return err
			}
			return printDeleted("server", *id)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			if err := st.DeleteServer(context.Background(), *id); err != nil {
				return err
			}
			return printDeleted("server", *id)
		})
	default:
		return fmt.Errorf("unknown server command %q", args[0])
	}
}

func principal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("principal requires add, update, delete, or list")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("principal add", flag.ContinueOnError)
		target := targetFlags(fs)
		name := fs.String("name", "", "principal name")
		kind := fs.String("kind", model.PrincipalAIAgent, "human, service, or ai-agent")
		notes := fs.String("notes", "", "notes")
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		in := model.Principal{Name: *name, Kind: *kind, Notes: *notes}
		if target.useAPI() {
			var item model.Principal
			if err := postAPI(target.apiURL, "/api/principals", in, &item); err != nil {
				return err
			}
			return printValue(item, *jsonOut)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			item, err := st.CreatePrincipal(context.Background(), in)
			if err != nil {
				return err
			}
			return printValue(item, *jsonOut)
		})
	case "list":
		fs := flag.NewFlagSet("principal list", flag.ContinueOnError)
		target := targetFlags(fs)
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if target.useAPI() {
			var items []model.Principal
			if err := getAPI(target.apiURL, "/api/principals", &items); err != nil {
				return err
			}
			return printPrincipals(items, *jsonOut)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			items, err := st.ListPrincipals(context.Background())
			if err != nil {
				return err
			}
			return printPrincipals(items, *jsonOut)
		})
	case "update":
		fs := flag.NewFlagSet("principal update", flag.ContinueOnError)
		target := targetFlags(fs)
		id := fs.Int64("id", 0, "principal id")
		name := fs.String("name", "", "principal name")
		kind := fs.String("kind", model.PrincipalAIAgent, "human, service, or ai-agent")
		notes := fs.String("notes", "", "notes")
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		in := model.Principal{ID: *id, Name: *name, Kind: *kind, Notes: *notes}
		if target.useAPI() {
			var item model.Principal
			if err := putAPI(target.apiURL, "/api/principals", in, &item); err != nil {
				return err
			}
			return printUpdated("principal", item.Name, *jsonOut, item)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			item, err := st.UpdatePrincipal(context.Background(), in)
			if err != nil {
				return err
			}
			return printUpdated("principal", item.Name, *jsonOut, item)
		})
	case "delete":
		fs := flag.NewFlagSet("principal delete", flag.ContinueOnError)
		target := targetFlags(fs)
		id := fs.Int64("id", 0, "principal id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if target.useAPI() {
			if err := deleteAPI(target.apiURL, "/api/principals", *id); err != nil {
				return err
			}
			return printDeleted("principal", *id)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			if err := st.DeletePrincipal(context.Background(), *id); err != nil {
				return err
			}
			return printDeleted("principal", *id)
		})
	default:
		return fmt.Errorf("unknown principal command %q", args[0])
	}
}

func access(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("access requires grant, update, delete, or list")
	}
	switch args[0] {
	case "grant":
		fs := flag.NewFlagSet("access grant", flag.ContinueOnError)
		target := targetFlags(fs)
		serverName := fs.String("server", "", "server name")
		principalName := fs.String("principal", "", "principal name")
		account := fs.String("account", "", "local account")
		sudo := fs.String("sudo", model.SudoNone, "none, prompted, or passwordless")
		notes := fs.String("notes", "", "notes")
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		in := model.AccessGrant{
			ServerName:    *serverName,
			PrincipalName: *principalName,
			Account:       *account,
			Sudo:          *sudo,
			Notes:         *notes,
		}
		if target.useAPI() {
			var item model.AccessGrant
			if err := postAPI(target.apiURL, "/api/access", in, &item); err != nil {
				return err
			}
			return printValue(item, *jsonOut)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			item, err := st.CreateAccessGrant(context.Background(), in)
			if err != nil {
				return err
			}
			return printValue(item, *jsonOut)
		})
	case "list":
		fs := flag.NewFlagSet("access list", flag.ContinueOnError)
		target := targetFlags(fs)
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if target.useAPI() {
			var items []model.AccessGrant
			if err := getAPI(target.apiURL, "/api/access", &items); err != nil {
				return err
			}
			return printAccessGrants(items, *jsonOut)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			items, err := st.ListAccessGrants(context.Background())
			if err != nil {
				return err
			}
			return printAccessGrants(items, *jsonOut)
		})
	case "update":
		fs := flag.NewFlagSet("access update", flag.ContinueOnError)
		target := targetFlags(fs)
		id := fs.Int64("id", 0, "access grant id")
		serverName := fs.String("server", "", "server name")
		principalName := fs.String("principal", "", "principal name")
		account := fs.String("account", "", "local account")
		sudo := fs.String("sudo", model.SudoNone, "none, prompted, or passwordless")
		notes := fs.String("notes", "", "notes")
		jsonOut := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		in := model.AccessGrant{
			ID:            *id,
			ServerName:    *serverName,
			PrincipalName: *principalName,
			Account:       *account,
			Sudo:          *sudo,
			Notes:         *notes,
		}
		if target.useAPI() {
			var item model.AccessGrant
			if err := putAPI(target.apiURL, "/api/access", in, &item); err != nil {
				return err
			}
			return printUpdated("access grant", fmt.Sprintf("%d", item.ID), *jsonOut, item)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			item, err := st.UpdateAccessGrant(context.Background(), in)
			if err != nil {
				return err
			}
			return printUpdated("access grant", fmt.Sprintf("%d", item.ID), *jsonOut, item)
		})
	case "delete":
		fs := flag.NewFlagSet("access delete", flag.ContinueOnError)
		target := targetFlags(fs)
		id := fs.Int64("id", 0, "access grant id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if target.useAPI() {
			if err := deleteAPI(target.apiURL, "/api/access", *id); err != nil {
				return err
			}
			return printDeleted("access grant", *id)
		}
		return withStore(target.dbPath, func(st *store.Store) error {
			if err := st.DeleteAccessGrant(context.Background(), *id); err != nil {
				return err
			}
			return printDeleted("access grant", *id)
		})
	default:
		return fmt.Errorf("unknown access command %q", args[0])
	}
}

func dbFlag(fs *flag.FlagSet) *string {
	defaultPath := os.Getenv("INSYLUS_DB")
	if defaultPath == "" {
		defaultPath = "insylus.db"
	}
	return fs.String("db", defaultPath, "database path")
}

type target struct {
	apiURL string
	dbPath string
}

func targetFlags(fs *flag.FlagSet) *target {
	apiURL := os.Getenv("INSYLUS_API")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:8097"
	}
	t := &target{}
	fs.StringVar(&t.apiURL, "api", apiURL, "API base URL")
	fs.StringVar(&t.dbPath, "db", os.Getenv("INSYLUS_DB"), "database path, bypassing API")
	return t
}

func (t *target) useAPI() bool {
	return t.dbPath == ""
}

func withStore(path string, fn func(*store.Store) error) error {
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()
	return fn(st)
}

func printValue(v any, jsonOut bool) error {
	if jsonOut {
		return printJSON(v)
	}
	switch item := v.(type) {
	case model.Server:
		fmt.Printf("server %s added\n", item.Name)
	case model.Principal:
		fmt.Printf("principal %s added\n", item.Name)
	case model.AccessGrant:
		fmt.Printf("access granted: %s -> %s as %s (%s sudo)\n", item.PrincipalName, item.ServerName, item.Account, item.Sudo)
	default:
		fmt.Println(v)
	}
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printUpdated(kind, name string, jsonOut bool, v any) error {
	if jsonOut {
		return printJSON(v)
	}
	fmt.Printf("%s %s updated\n", kind, name)
	return nil
}

func printDeleted(kind string, id int64) error {
	fmt.Printf("%s %d deleted\n", kind, id)
	return nil
}

func printServers(items []model.Server, jsonOut bool) error {
	if jsonOut {
		return printJSON(items)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tHOSTNAME\tADDRESS\tNOTES")
	for _, item := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.Name, item.Hostname, item.Address, item.Notes)
	}
	return tw.Flush()
}

func printPrincipals(items []model.Principal, jsonOut bool) error {
	if jsonOut {
		return printJSON(items)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tNOTES")
	for _, item := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", item.Name, item.Kind, item.Notes)
	}
	return tw.Flush()
}

func printAccessGrants(items []model.AccessGrant, jsonOut bool) error {
	if jsonOut {
		return printJSON(items)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVER\tPRINCIPAL\tACCOUNT\tSUDO\tNOTES")
	for _, item := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", item.ServerName, item.PrincipalName, item.Account, item.Sudo, item.Notes)
	}
	return tw.Flush()
}

func getAPI(baseURL, path string, out any) error {
	resp, err := http.Get(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeAPI(resp, out)
}

func postAPI(baseURL, path string, in any, out any) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(in); err != nil {
		return err
	}
	resp, err := http.Post(strings.TrimRight(baseURL, "/")+path, "application/json", &body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeAPI(resp, out)
}

func putAPI(baseURL, path string, in any, out any) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(in); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, strings.TrimRight(baseURL, "/")+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeAPI(resp, out)
}

func deleteAPI(baseURL, path string, id int64) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s%s?id=%d", strings.TrimRight(baseURL, "/"), path, id), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeAPI(resp, &struct{}{})
}

func decodeAPI(resp *http.Response, out any) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Error != "" {
			return fmt.Errorf("API returned %s: %s", resp.Status, apiErr.Error)
		}
		return fmt.Errorf("API returned %s", resp.Status)
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.Unmarshal(data, out)
}

func usage() error {
	fmt.Fprintln(os.Stdout, strings.TrimSpace(`
Insylus

Usage:
  insylus serve [--db PATH] [--listen ADDR]
  insylus server add --name NAME [--host HOST] [--addr ADDR] [--notes TEXT] [--api URL|--db PATH]
  insylus server update --id ID --name NAME [--host HOST] [--addr ADDR] [--notes TEXT] [--api URL|--db PATH]
  insylus server delete --id ID [--api URL|--db PATH]
  insylus server list [--json] [--api URL|--db PATH]
  insylus principal add --name NAME [--kind human|service|ai-agent] [--notes TEXT] [--api URL|--db PATH]
  insylus principal update --id ID --name NAME [--kind human|service|ai-agent] [--notes TEXT] [--api URL|--db PATH]
  insylus principal delete --id ID [--api URL|--db PATH]
  insylus principal list [--json] [--api URL|--db PATH]
  insylus access grant --server SERVER --principal PRINCIPAL --account ACCOUNT [--sudo none|prompted|passwordless] [--notes TEXT] [--api URL|--db PATH]
  insylus access update --id ID --server SERVER --principal PRINCIPAL --account ACCOUNT [--sudo none|prompted|passwordless] [--notes TEXT] [--api URL|--db PATH]
  insylus access delete --id ID [--api URL|--db PATH]
  insylus access list [--json] [--api URL|--db PATH]
`))
	return nil
}

func listenURL(listen string) string {
	if strings.HasPrefix(listen, ":") {
		return "http://0.0.0.0" + listen
	}
	return "http://" + listen
}
