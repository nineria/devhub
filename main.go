// ostler supervises the local services, runs the health checks and serves a phone-friendly page to
// control them. One binary; the page is embedded. Listens only on the addresses in OSTLER_BIND.
package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed all:web/dist
var uiFiles embed.FS

// getEnv reads OSTLER_<name>. The old DEVHUB_<name> still works, so a setup from before the rename keeps running.
func getEnv(name string) string {
	if v := os.Getenv("OSTLER_" + name); v != "" {
		return v
	}
	return os.Getenv("DEVHUB_" + name)
}

func envOr(name, fallback string) string {
	if v := getEnv(name); v != "" {
		return v
	}
	return fallback
}

// configDir is ~/.config/ostler, or ~/.config/devhub while that is the only one that exists.
func configDir() string {
	if old := expandHome("~/.config/devhub"); !fileExists(expandHome("~/.config/ostler")) && fileExists(old) {
		return old
	}
	return expandHome("~/.config/ostler")
}

func configPath() string {
	if p := getEnv("CONFIG"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		for _, name := range []string{"ostler.json", "devhub.json"} {
			if p := filepath.Join(filepath.Dir(exe), name); fileExists(p) {
				return p
			}
		}
	}
	if fileExists("devhub.json") && !fileExists("ostler.json") {
		return "devhub.json"
	}
	return "ostler.json"
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// serve listens on every address in binds, retrying if one is not up yet (a VPN address may come up after us).
func serve(name string, binds []string, port int, h http.Handler) {
	for _, host := range binds {
		addr := net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(port))
		go func() {
			for {
				ln, err := net.Listen("tcp", addr)
				if err != nil {
					log.Printf("%s: cannot listen on %s (%v); trying again in 5s", name, addr, err)
					time.Sleep(5 * time.Second)
					continue
				}
				log.Printf("%s: listening on http://%s", name, addr)
				if err := (&http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}).Serve(ln); err != nil {
					log.Printf("%s: %s stopped: %v", name, addr, err)
				}
				time.Sleep(5 * time.Second)
			}
		}()
	}
}

func main() {
	cfgPath := configPath()
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	// Secrets (tokens, bot keys) live in env files, not in the config: KEY=value lines, one per line.
	env := childEnv()
	for _, f := range strings.Split(envOr("ENV_FILES", filepath.Join(configDir(), "env")), ",") {
		env = append(env, readEnvFile(strings.TrimSpace(f))...)
	}
	autostart := getEnv("AUTOSTART") != "0"

	// The services you disabled stay disabled after a restart.
	statePath := expandHome(envOr("STATE", filepath.Join(configDir(), "state.json")))
	disabled := loadDisabled(statePath)
	binds := strings.Split(envOr("BIND", "127.0.0.1"), ",")

	hub := &Hub{items: map[string]item{}}
	hub.onChange = func() { saveDisabled(statePath, hub) }
	// Editing from the page lets a request set a command, so it is on by default only when nothing but
	// this machine can reach the hub. OSTLER_EDIT=1 or 0 overrides that.
	canEdit := loopbackOnly(binds)
	switch getEnv("EDIT") {
	case "1":
		canEdit = true
	case "0":
		canEdit = false
	}
	hosts := strings.Split(getEnv("HOSTS"), ",")
	for _, b := range binds {
		hosts = append(hosts, strings.Trim(strings.TrimSpace(b), "[]"))
	}
	hub.edit = newEditor(cfgPath, cfg, env, autostart, hosts)
	for _, s := range cfg.Services {
		hub.order = append(hub.order, s.ID)
		hub.items[s.ID] = hub.edit.spawn(s, nil, disabled[s.ID], autostart)
	}
	if !canEdit { // the editor was needed to start the services; from here on the page may not use it
		hub.edit = nil
	}

	ui, err := fs.Sub(uiFiles, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	// OSTLER_UI serves the page from a folder instead of the copy inside the binary, so a page change
	// needs only a rebuild of the page, not a restart of the hub and the services it runs.
	if dir := getEnv("UI"); dir != "" {
		ui = os.DirFS(expandHome(dir))
	}
	port, _ := strconv.Atoi(envOr("PORT", "8090"))
	hub.urls = accessURLs(binds, port)
	serve("hub", binds, port, hub.routes(ui))
	log.Printf("open Ostler in your browser: %s", strings.Join(hub.urls, "  or  "))

	// OSTLER_PROXIES="8082=http://127.0.0.1:8081,..." also exposes other local web pages on the same
	// addresses as the hub, so one VPN address reaches all of them.
	for _, spec := range strings.Split(getEnv("PROXIES"), ",") {
		portText, target, ok := strings.Cut(strings.TrimSpace(spec), "=")
		p, perr := strconv.Atoi(portText)
		u, uerr := url.Parse(target)
		if !ok || perr != nil || uerr != nil {
			if strings.TrimSpace(spec) != "" {
				log.Printf("OSTLER_PROXIES: ignoring %q (expected PORT=URL)", spec)
			}
			continue
		}
		serve("proxy :"+portText, binds, p, httputil.NewSingleHostReverseProxy(u))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Print("stopping services")
	hub.editMu.Lock() // no edit may start a process while we shut down
	var runners []*Runner
	for _, it := range hub.items {
		if it.runner != nil {
			runners = append(runners, it.runner)
		}
	}
	for _, r := range runners {
		r.Stop()
	}
	deadline := time.Now().Add(12 * time.Second)
	for _, r := range runners {
		for time.Now().Before(deadline) && r.View().Pid != 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

type savedState struct {
	Disabled []string `json:"disabled"`
}

func loadDisabled(path string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var st savedState
	if json.Unmarshal(raw, &st) == nil {
		for _, id := range st.Disabled {
			out[id] = true
		}
	}
	return out
}

func saveDisabled(path string, hub *Hub) {
	st := savedState{Disabled: []string{}}
	hub.mu.RLock()
	for _, id := range hub.order {
		if hub.items[id].view().Disabled {
			st.Disabled = append(st.Disabled, id)
		}
	}
	hub.mu.RUnlock()
	raw, _ := json.MarshalIndent(st, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
}
