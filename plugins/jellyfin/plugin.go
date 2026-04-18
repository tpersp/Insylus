package jellyfin

import (
	"embed"
	"io/fs"

	"insylus/internal/ctl"
	"insylus/internal/pluginhost"
)

//go:embed templates/*.html
var files embed.FS

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "jellyfin"
}

func (Plugin) Name() string {
	return "Jellyfin"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(files, ".")
		if err != nil {
			return err
		}
		rt := runtime{store: newStore(host), render: host.Web().Render}
		host.Web().NavItem(pluginhost.NavItem{Label: "Jellyfin", Href: "/jellyfin", Order: 45})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /jellyfin", rt.handleJellyfinPage)
		host.Web().HandleFunc("POST /jellyfin/tokens", rt.handleJellyfinSetToken)
		host.Web().HandleFunc("POST /jellyfin/tokens/{device_id}/delete", rt.handleJellyfinDeleteToken)
	}
	if host.API().Enabled() {
		rt := runtime{store: newStore(host)}
		host.API().HandleFunc("GET /api/jellyfin/servers", rt.handleJellyfinServers)
		host.API().HandleFunc("POST /api/jellyfin/tokens", rt.handleJellyfinSetToken)
		host.API().HandleFunc("POST /api/jellyfin/tokens/delete/{device_id}", rt.handleJellyfinDeleteToken)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/libraries", rt.handleJellyfinLibraries)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/items", rt.handleJellyfinItems)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/movies", rt.handleJellyfinMovies)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/series", rt.handleJellyfinSeries)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/episodes", rt.handleJellyfinEpisodes)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/latest", rt.handleJellyfinLatest)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/resume", rt.handleJellyfinResume)
		host.API().HandleFunc("GET /api/jellyfin/{device_id}/items/{item_id}", rt.handleJellyfinItem)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "jellyfin",
			Version:  1,
			Name:     "create jellyfin token table",
			SQL: `
create table if not exists jellyfin_tokens (
	device_id text primary key references targets(id) on delete cascade,
	server_name text not null,
	api_url text not null default '',
	api_key_encrypted text not null,
	default_user_id text not null default '',
	default_username text not null default '',
	tls_insecure integer not null default 0,
	created_at text not null,
	updated_at text not null
);
`,
		})
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "jellyfin",
			Version:  2,
			Name:     "add user columns to jellyfin tokens",
			SQL: `
alter table jellyfin_tokens add column default_user_id text not null default '';
alter table jellyfin_tokens add column default_username text not null default '';
`,
		})
	}
	return nil
}

func command() ctl.Command {
	return ctl.Command{
		Name:  "jellyfin",
		Usage: "jellyfin [--server URL] --device DEVICE [--list|--movies|--series|--episodes|--latest|--resume] [--json]",
		Short: "Query Jellyfin library items",
		Long:  "Use user-provided Jellyfin API keys stored in Insylus to list movies, series, episodes, and playback status.",
		Examples: []string{
			"jellyfin --device my-server --list",
			"jellyfin --device my-server --movies",
			"jellyfin --device my-server --series",
			"jellyfin --device my-server --episodes",
			"jellyfin --device my-server --latest",
			"jellyfin --device my-server --resume",
			"jellyfin set-token --device my-server --api-url \"http://jellyfin:8096\" --api-key \"your-api-key\"",
			"jellyfin list-tokens",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}

// PluginSkill is the OpenClaw skill contribution for this plugin.
// It is read by the deploy script to update the insylus SKILL.md.
