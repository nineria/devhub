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
	Method   string `json:"method"`   // default POST
	TokenEnv string `json:"tokenEnv"` // optional: the variable holding a bearer token to send
}

// Service is one thing the hub shows: a supervised process, a periodic check, or an on-demand task.
// Commands come only from this file, never from a request.
type Service struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Group     string     `json:"group"` // any name; services with the same group are listed together
	Kind      string     `json:"kind"`  // process, check, task
	Desc      string     `json:"desc"`
	Dir       string     `json:"dir"`
	Build     string     `json:"build"`
	Run       string     `json:"run"`
	Watch     []string   `json:"watch"`
	Ready     string     `json:"ready"`
	Check     string     `json:"check"`
	Interval  int        `json:"interval"`
	Heartbeat *Heartbeat `json:"heartbeat"`
	Buttons   []Button   `json:"buttons"`
	Links     []Link     `json:"links"`
}

type Config struct {
	Root     string    `json:"root"`
	Services []Service `json:"services"`
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
	c.Root = expandHome(c.Root)
	seen := map[string]bool{}
	for _, s := range c.Services {
		if s.ID == "" || seen[s.ID] {
			return nil, fmt.Errorf("service id %q is empty or repeated", s.ID)
		}
		seen[s.ID] = true
		switch s.Kind {
		case "process":
			if s.Run == "" {
				return nil, fmt.Errorf("%s: a process needs run", s.ID)
			}
		case "check", "task":
			if s.Check == "" {
				return nil, fmt.Errorf("%s: needs check", s.ID)
			}
		default:
			return nil, fmt.Errorf("%s: unknown kind %q", s.ID, s.Kind)
		}
	}
	return &c, nil
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
