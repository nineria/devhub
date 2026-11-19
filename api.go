package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ButtonView struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty"`
}

// BuildView is the result of the last build.
type BuildView struct {
	OK bool  `json:"ok"`
	At int64 `json:"at"` // unix seconds
	Ms int64 `json:"ms"`
}

// View is what the page sees of one service.
type View struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Group    string       `json:"group"`
	Kind     string       `json:"kind"`
	Desc     string       `json:"desc"`
	State    string       `json:"state"`
	Ready    bool         `json:"ready"`
	Detail   string       `json:"detail,omitempty"`
	Note     string       `json:"note,omitempty"`
	Busy     string       `json:"busy,omitempty"`
	Since    int64        `json:"since"`
	Restarts int          `json:"restarts"`
	Want     bool         `json:"want"`
	Pid      int          `json:"pid,omitempty"`
	Disabled bool         `json:"disabled"`
	Errors   int          `json:"errors"`
	Warnings int          `json:"warnings"`
	Build    *BuildView   `json:"build,omitempty"`
	Blocker  *Blocker     `json:"blocker,omitempty"` // what holds the service's port
	Buttons  []ButtonView `json:"buttons"`
	Links    []Link       `json:"links"`
}

type item struct {
	runner  *Runner
	checker *Checker
}

func (i item) logs() *LogBuf {
	if i.runner != nil {
		return i.runner.Logs()
	}
	return i.checker.Logs()
}

func (i item) disable() {
	if i.runner != nil {
		i.runner.Disable()
	} else {
		i.checker.Disable()
	}
}

func (i item) enable() {
	if i.runner != nil {
		i.runner.Enable()
	} else {
		i.checker.Enable()
	}
}

func (i item) view() View {
	if i.runner != nil {
		return i.runner.View()
	}
	return i.checker.View()
}

type Hub struct {
	mu    sync.RWMutex // guards order and items, which the page can change
	order []string
	items map[string]item
	// editMu serialises add, change and delete, which wait for the old process to stop.
	editMu sync.Mutex
	// edit is nil when editing from the page is turned off.
	edit *editor
	// onChange is called after a service is disabled or enabled, so the choice survives a restart.
	onChange func()
	// urls are the addresses the page can be opened at, shown in the page and at startup.
	urls []string
}

// streamLogs sends a log's lines as server-sent events until the reader goes away.
func streamLogs(w http.ResponseWriter, r *http.Request, buf *LogBuf) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	backlog, ch, cancel := buf.Subscribe()
	defer cancel()
	send := func(line Line) {
		b, _ := json.Marshal(line)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	for _, l := range backlog {
		send(l)
	}
	fl.Flush()
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case l := <-ch:
			send(l)
			fl.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// needsHeader keeps a web page open on the phone from pressing buttons here: a request from another
// origin cannot add this header without a preflight, which the hub never answers.
func needsHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ostler") != "1" {
			jsonOut(w, http.StatusForbidden, map[string]any{"ok": false, "error": "forbidden"})
			return
		}
		next(w, r)
	}
}

func (h *Hub) routes(ui fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/services", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"services": h.views(), "now": time.Now().Unix()})
	})

	mux.HandleFunc("GET /api/services/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		it, ok := h.get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		streamLogs(w, r, it.logs())
	})
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) { streamLogs(w, r, allLogs) })

	act := func(w http.ResponseWriter, r *http.Request, fn func(item) (string, error)) {
		it, ok := h.get(r.PathValue("id"))
		if !ok {
			jsonOut(w, 404, map[string]any{"ok": false, "error": "unknown service"})
			return
		}
		out, err := fn(it)
		switch {
		case errors.Is(err, errBusy):
			jsonOut(w, 409, map[string]any{"ok": false, "error": err.Error()})
		case err != nil:
			jsonOut(w, 200, map[string]any{"ok": false, "error": err.Error(), "output": out})
		default:
			jsonOut(w, 200, map[string]any{"ok": true, "output": out})
		}
	}
	processOnly := func(it item) error {
		if it.runner == nil {
			return errors.New("this service can't be started or stopped")
		}
		return nil
	}
	mux.HandleFunc("POST /api/services/{id}/start", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			if err := processOnly(it); err != nil {
				return "", err
			}
			if it.runner.View().Disabled {
				return "", errors.New("this service is disabled; enable it first")
			}
			it.runner.Start()
			return "", nil
		})
	}))
	mux.HandleFunc("POST /api/services/{id}/stop", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			if err := processOnly(it); err != nil {
				return "", err
			}
			it.runner.Stop()
			return "", nil
		})
	}))
	mux.HandleFunc("POST /api/services/{id}/restart", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			if err := processOnly(it); err != nil {
				return "", err
			}
			it.runner.Restart()
			return "", nil
		})
	}))
	mux.HandleFunc("POST /api/services/{id}/free-port", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			if err := processOnly(it); err != nil {
				return "", err
			}
			return "", it.runner.FreePort()
		})
	}))
	mux.HandleFunc("POST /api/services/{id}/disable", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			it.disable()
			if h.onChange != nil {
				h.onChange()
			}
			return "", nil
		})
	}))
	mux.HandleFunc("POST /api/services/{id}/enable", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			it.enable()
			if h.onChange != nil {
				h.onChange()
			}
			return "", nil
		})
	}))
	mux.HandleFunc("POST /api/services/{id}/clear", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			it.logs().Clear()
			return "", nil
		})
	}))
	mux.HandleFunc("POST /api/logs/clear", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		allLogs.Clear()
		jsonOut(w, 200, map[string]any{"ok": true})
	}))
	mux.HandleFunc("POST /api/services/{id}/buttons/{button}", needsHeader(func(w http.ResponseWriter, r *http.Request) {
		act(w, r, func(it item) (string, error) {
			if it.checker == nil {
				return "", errors.New("this service has no buttons")
			}
			return it.checker.RunButton(r.PathValue("button"))
		})
	}))

	h.manageRoutes(mux)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	files := http.FileServer(http.FS(ui))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(ui, path); err != nil {
			// A client-side route: serve the page.
			r.URL.Path = "/"
			path = "index.html"
		}
		if path == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
	return mux
}
