package server

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"insylus/internal/httpx"
	"insylus/internal/pluginhost"
	"insylus/internal/shared"
	"insylus/plugins/registry"
)

//go:embed templates/*.html static/*
var embeddedFiles embed.FS

const maxRequestBodyBytes = 10 << 20

type Config struct {
	ListenAddr      string
	DBPath          string
	PublicBaseURL   string
	AgentBinaryPath string
	ManagedUser     string
	ManagedGroups   []string
}

type App struct {
	store            *Store
	cfg              Config
	templates        *template.Template
	staticHandler    http.Handler
	logger           *log.Logger
	apiRoutes        []routeDef
	webRoutes        []routeDef
	navItems         []pluginhost.NavItem
	staticMounts     []pluginhost.StaticMount
	templateSets     []pluginhost.TemplateSet
	pluginMigrations []pluginhost.Migration
	capabilities     *capabilityRegistry
	plugins          pluginRuntime
}

func New(cfg Config, logger *log.Logger) (*App, error) {
	store, err := OpenStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	app := &App{
		store:         store,
		cfg:           cfg,
		staticHandler: http.FileServer(http.FS(embeddedFiles)),
		logger:        logger,
		capabilities:  newCapabilityRegistry(),
	}
	app.capabilities.Provide("managed_account_config_provider", app)
	app.capabilities.Provide("device_admin_service", deviceAdminService{store: store, targets: store.targetService()})
	app.capabilities.Provide("agent_controller_service", agentControllerService{app: app})
	plugins := registry.Plugins()
	if err := store.EnsurePluginSettings(context.Background(), plugins); err != nil {
		return nil, err
	}
	app.plugins = newPluginRuntime(app, plugins)
	app.registerCoreRoutes()
	host := newServerPluginHost(app)
	for _, plugin := range plugins {
		if err := plugin.Register(host.ForPlugin(plugin.ID())); err != nil {
			return nil, err
		}
	}
	if err := app.applyPluginMigrations(context.Background()); err != nil {
		return nil, err
	}
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "never"
			}
			return t.Local().Format("2006-01-02 15:04:05")
		},
		"formatOptionalTime": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return "never"
			}
			return t.Local().Format("2006-01-02 15:04:05")
		},
		"staleClass": func(t time.Time) string {
			if t.IsZero() {
				return "state-stale"
			}
			if time.Since(t) > shared.DeviceOnlineWindow {
				return "state-stale"
			}
			return "state-good"
		},
		"isOnline": func(t time.Time) bool {
			return deviceIsOnline(t)
		},
		"agentHealthLabel": func(t time.Time) string {
			if t.IsZero() {
				return "unknown"
			}
			if deviceIsOnline(t) {
				return "healthy"
			}
			return "offline"
		},
		"agentHealthClass": func(t time.Time) string {
			if t.IsZero() {
				return "pill-muted"
			}
			if deviceIsOnline(t) {
				return "pill-ok"
			}
			return "pill-danger"
		},
		"agentUpdateLabel": func(info shared.AgentUpdateInfo) string {
			switch info.Status {
			case shared.AgentUpdateStatusFailed:
				return "failed"
			case shared.AgentUpdateStatusUnsupported:
				return "unsupported"
			case shared.AgentUpdateStatusUpdating:
				return "updating"
			}
			if info.UpdateAvailable {
				return "update available"
			}
			if info.Status == shared.AgentUpdateStatusUpdated || info.ServerVersion != "" {
				return "up to date"
			}
			return "idle"
		},
		"agentUpdateClass": func(info shared.AgentUpdateInfo) string {
			switch info.Status {
			case shared.AgentUpdateStatusFailed:
				return "pill-danger"
			case shared.AgentUpdateStatusUnsupported:
				return "pill-muted"
			case shared.AgentUpdateStatusUpdating, shared.AgentUpdateStatusAvailable:
				return "pill-warn"
			}
			if info.UpdateAvailable {
				return "pill-warn"
			}
			return "pill-ok"
		},
		"isSelectedMode": func(current shared.AccessMode, want string) bool {
			return string(current) == want
		},
		"isSelectedDeviceMode": func(current shared.DeviceMode, want string) bool {
			return string(current) == want
		},
		"isSelectedKey": func(current *int64, want int64) bool {
			return current != nil && *current == want
		},
		"isSelectedDeviceType": func(current shared.DeviceType, want string) bool {
			return string(current) == want
		},
		"pluginNavItems": app.pluginNavItems,
	}
	if err := app.parseTemplates(funcs); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) Close() error {
	return a.store.Close()
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	for _, mount := range a.staticMounts {
		mux.Handle("GET "+mount.Prefix, a.pluginGate(mount.PluginID, http.StripPrefix(mount.Prefix, http.FileServer(http.FS(mount.FS)))))
	}
	for _, route := range a.apiRoutes {
		mux.Handle(route.Pattern, a.pluginGate(route.PluginID, http.HandlerFunc(route.Handler)))
	}
	for _, route := range a.webRoutes {
		mux.Handle(route.Pattern, a.pluginGate(route.PluginID, http.HandlerFunc(route.Handler)))
	}
	return a.withBaseMiddleware(mux)
}

func (a *App) withBaseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) pluginGate(pluginID string, next http.Handler) http.Handler {
	if pluginID == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.plugins.Enabled(pluginID) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (a *App) baseURL(r *http.Request) string {
	if a.cfg.PublicBaseURL != "" {
		return strings.TrimRight(a.cfg.PublicBaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func (a *App) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return httpx.DecodeJSON(w, r, dst)
}

func (a *App) writeJSON(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSON(w, status, v)
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	httpx.MethodNotAllowed(w, r)
}
