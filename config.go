package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Button struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty"`
	Cmd     string `json:"cmd"`
}

type Link struct {
	Label string `json:"label"`
	Path  string `json:"path"` // ":8765" or "/path"; the UI joins it with the host you opened the hub from
}

// Heartbeat reports a passing check to another monitor (Gatus, Uptime Kuma, healthchecks.io, ...), which
// raises the alarm when the heartbeats stop. ${NAME} in the URL is filled from the environment, so secrets
// stay out of the config file.
type Heartbeat struct {
	URL      string `json:"url"`
	Method   string `json:"method,omitempty"`   // default POST
	TokenEnv string `json:"tokenEnv,omitempty"` // optional: the variable holding a bearer token to send
}

// Service is one thing the hub shows: a supervised process, a periodic check, or an on-demand task.
// Commands come only from this file, never from a request.
type Service struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Group     string     `json:"group,omitempty"` // any name; services with the same group are listed together
	Kind      string     `json:"kind"`            // process, check, task
	Desc      string     `json:"desc,omitempty"`
	Dir       string     `json:"dir,omitempty"`
	Build     string     `json:"build,omitempty"`
	Run       string     `json:"run,omitempty"`
	Watch     []string   `json:"watch,omitempty"`
	Ready     string     `json:"ready,omitempty"`
	Check     string     `json:"check,omitempty"`
	Interval  int        `json:"interval,omitempty"`
	Heartbeat *Heartbeat `json:"heartbeat,omitempty"`
	Buttons   []Button   `json:"buttons,omitempty"`
	Links     []Link     `json:"links,omitempty"`
}

type Config struct {
	Root     string    `json:"root"` // "~" expanded after loading
	Services []Service `json:"services"`
	RawRoot  string    `json:"-"` // as written in the file, so saving keeps "~"
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.RawRoot = c.Root
	c.Root = expandHome(c.Root)
	if err := validate(c.Services); err != nil {
		return nil, err
	}
	return &c, nil
}

// validate checks the whole list of services, on load and before every save from the page.
func validate(services []Service) error {
	seen := map[string]bool{}
	for _, s := range services {
		if s.ID == "" || seen[s.ID] {
			return fmt.Errorf("service id %q is empty or repeated", s.ID)
		}
		if strings.ContainsAny(s.ID, " \t\r\n/\\#?%") {
			return fmt.Errorf("%s: an id can't contain spaces or / \\ # ? %%", s.ID)
		}
		seen[s.ID] = true
		switch s.Kind {
		case "process":
			if s.Run == "" {
				return fmt.Errorf("%s: a process needs run", s.ID)
			}
		case "check", "task":
			if s.Check == "" {
				return fmt.Errorf("%s: needs check", s.ID)
			}
			if s.Interval < 0 {
				return fmt.Errorf("%s: interval can't be negative", s.ID)
			}
		default:
			return fmt.Errorf("%s: unknown kind %q", s.ID, s.Kind)
		}
		ids := map[string]bool{}
		for _, b := range s.Buttons {
			if b.ID == "" || b.Cmd == "" || ids[b.ID] {
				return fmt.Errorf("%s: every button needs a command and its own id", s.ID)
			}
			ids[b.ID] = true
		}
	}
	return nil
}

// readEnvFile reads KEY="value" lines (as written by the other tools) into the process environment list.
func readEnvFile(path string) []string {
	raw, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		out = append(out, strings.TrimSpace(k)+"="+strings.Trim(strings.TrimSpace(v), `"'`))
	}
	return out
}

// childEnv is the environment every command runs in. launchd and Tilt start with a short PATH, so the
// tool directories are added explicitly.
func childEnv() []string {
	home, _ := os.UserHomeDir()
	extra := []string{
		"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin",
		filepath.Join(home, ".cargo/bin"), filepath.Join(home, ".local/share/vite-plus/bin"),
		filepath.Join(home, "go/bin"), filepath.Join(home, ".orbstack/bin"),
	}
	env := os.Environ()
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + strings.Join(extra, ":") + ":" + strings.TrimPrefix(kv, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+strings.Join(extra, ":")+":/usr/bin:/bin:/usr/sbin:/sbin")
}
