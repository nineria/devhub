// devhub supervises the local services, runs the health checks and serves a phone-friendly page to
// control them. One binary; the page is embedded. Listens only on the addresses in DEVHUB_BIND.
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func configPath() string {
	if p := os.Getenv("DEVHUB_CONFIG"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "devhub.json"); fileExists(p) {
			return p
		}
	}
	return "devhub.json"
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
	cfg, err := loadConfig(configPath())
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	// Secrets (tokens, bot keys) live in env files, not in the config: KEY=value lines, one per line.
	env := childEnv()
	for _, f := range strings.Split(envOr("DEVHUB_ENV_FILES", "~/.config/devhub/env"), ",") {
		env = append(env, readEnvFile(strings.TrimSpace(f))...)
	}
	autostart := os.Getenv("DEVHUB_AUTOSTART") != "0"

	// The services you disabled stay disabled after a restart.
	statePath := expandHome(envOr("DEVHUB_STATE", "~/.config/devhub/state.json"))
	disabled := loadDisabled(statePath)
	hub := &Hub{items: map[string]item{}}
	hub.onChange = func() { saveDisabled(statePath, hub) }
	var runners []*Runner
	for _, s := range cfg.Services {
		hub.order = append(hub.order, s.ID)
		if s.Kind == "process" {
			r := newRunner(s, cfg.Root, env)
			r.Run()
			if disabled[s.ID] {
				r.Disable()
			} else if autostart {
				r.Start()
			}
			runners = append(runners, r)
			hub.items[s.ID] = item{runner: r}
			continue
		}
		c := newChecker(s, cfg.Root, env)
		if disabled[s.ID] {
			c.Disable()
		}
		c.Run()
		hub.items[s.ID] = item{checker: c}
	}

	ui, err := fs.Sub(uiFiles, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	// DEVHUB_UI serves the page from a folder instead of the copy inside the binary, so a page change
	// needs only a rebuild of the page, not a restart of the hub and the services it runs.
	if dir := os.Getenv("DEVHUB_UI"); dir != "" {
		ui = os.DirFS(expandHome(dir))
	}
	binds := strings.Split(envOr("DEVHUB_BIND", "127.0.0.1"), ",")
	port, _ := strconv.Atoi(envOr("DEVHUB_PORT", "8090"))
	serve("hub", binds, port, hub.routes(ui))

	// DEVHUB_PROXIES="8082=http://127.0.0.1:8081,..." also exposes other local web pages on the same
	// addresses as the hub, so one VPN address reaches all of them.
	for _, spec := range strings.Split(os.Getenv("DEVHUB_PROXIES"), ",") {
		portText, target, ok := strings.Cut(strings.TrimSpace(spec), "=")
		p, perr := strconv.Atoi(portText)
		u, uerr := url.Parse(target)
		if !ok || perr != nil || uerr != nil {
			if strings.TrimSpace(spec) != "" {
				log.Printf("DEVHUB_PROXIES: ignoring %q (expected PORT=URL)", spec)
			}
			continue
		}
		serve("proxy :"+portText, binds, p, httputil.NewSingleHostReverseProxy(u))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Print("stopping services")
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
	for _, id := range hub.order {
		if hub.items[id].view().Disabled {
			st.Disabled = append(st.Disabled, id)
		}
	}
	raw, _ := json.MarshalIndent(st, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
}
