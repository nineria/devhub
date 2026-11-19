package main

// Editing services from the page. The page may add, change and delete services, which writes ostler.json
// and swaps the running service. This lets a request set a command, so it is only available when the hub
// is reachable from this machine alone (or when OSTLER_EDIT=1 says otherwise), and every request must
// carry the X-Ostler header and come from a known Host and the same origin.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var errChanged = errors.New("ostler.json changed outside the page. Restart Ostler to load it, then try again")

// editor holds what the hub needs to start services and to write the config back.
type editor struct {
	path      string
	rawRoot   string // "root" as written in the file
	root      string // expanded
	env       []string
	autostart bool
	hosts     map[string]bool // Host names a request may use

	mtime time.Time // of the file as we last read or wrote it
	size  int64
}

func newEditor(path string, cfg *Config, env []string, autostart bool, hosts []string) *editor {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	e := &editor{path: path, rawRoot: cfg.RawRoot, root: cfg.Root, env: env, autostart: autostart,
		hosts: map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}}
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			e.hosts[h] = true
		}
	}
	e.stamp()
	return e
}

func (e *editor) stamp() {
	if fi, err := os.Stat(e.path); err == nil {
		e.mtime, e.size = fi.ModTime(), fi.Size()
	}
}

// write saves the services atomically, unless someone edited the file by hand since we read it.
func (e *editor) write(services []Service) error {
	if fi, err := os.Stat(e.path); err == nil && (!fi.ModTime().Equal(e.mtime) || fi.Size() != e.size) {
		return errChanged
	}
	if services == nil {
		services = []Service{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		Root     string    `json:"root"`
		Services []Service `json:"services"`
	}{e.rawRoot, services}); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(e.path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(e.path), ".ostler-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), e.path); err != nil {
		return err
	}
	e.stamp()
	return nil
}

// spawn creates and starts the runner or checker for a service. A kept log carries the history over.
func (e *editor) spawn(s Service, keep *LogBuf, disabled, start bool) item {
	var it item
	if s.Kind == "process" {
		r := newRunner(s, e.root, e.env)
		if keep != nil {
			r.log = keep
		}
		r.Run()
		if disabled {
			r.Disable()
		} else if start {
			r.Start()
		}
		it = item{runner: r}
	} else {
		c := newChecker(s, e.root, e.env)
		if keep != nil {
			c.log = keep
		}
		if disabled {
			c.Disable()
		}
		c.Run()
		it = item{checker: c}
	}
	return it
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func cleanList(in []string) []string {
	var out []string
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// normalize trims what the form sends and drops the fields that don't belong to the kind.
func normalize(s Service) Service {
	t := strings.TrimSpace
	s.ID, s.Name, s.Group, s.Kind, s.Desc = t(s.ID), t(s.Name), t(s.Group), t(s.Kind), t(s.Desc)
	s.Dir, s.Build, s.Run, s.Ready, s.Check = t(s.Dir), t(s.Build), t(s.Run), t(s.Ready), t(s.Check)
	if s.Name == "" {
		s.Name = s.ID
	}
	s.Watch = cleanList(s.Watch)
	if s.Kind == "process" {
		s.Check, s.Interval, s.Heartbeat = "", 0, nil
	} else {
		s.Build, s.Run, s.Watch, s.Ready, s.Port = "", "", nil, "", 0
	}
	if s.Kind == "task" {
		s.Interval, s.Heartbeat = 0, nil
	}
	if s.Heartbeat != nil {
		s.Heartbeat.URL, s.Heartbeat.Method, s.Heartbeat.TokenEnv =
			t(s.Heartbeat.URL), strings.ToUpper(t(s.Heartbeat.Method)), t(s.Heartbeat.TokenEnv)
		if s.Heartbeat.URL == "" {
			s.Heartbeat = nil
		}
	}
	var buttons []Button
	used := map[string]bool{}
	for _, b := range s.Buttons {
		b.ID, b.Label, b.Confirm, b.Cmd = t(b.ID), t(b.Label), t(b.Confirm), t(b.Cmd)
		if b.Label == "" && b.Cmd == "" {
			continue // an empty row
		}
		if b.Label == "" {
			b.Label = b.ID
		}
		if b.ID == "" {
			b.ID = slug(b.Label)
		}
		base := b.ID
		for n := 2; used[b.ID]; n++ {
			b.ID = fmt.Sprintf("%s-%d", base, n)
		}
		used[b.ID] = true
		buttons = append(buttons, b)
	}
	s.Buttons = buttons
	var links []Link
	for _, l := range s.Links {
		l.Label, l.Path = t(l.Label), t(l.Path)
		if l.Label == "" && l.Path == "" {
			continue
		}
		if l.Label == "" {
			l.Label = l.Path
		}
		links = append(links, l)
	}
	s.Links = links
	return s
}

// ---- the hub's list of services, safe to read while the page edits it ----

func (h *Hub) get(id string) (item, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	it, ok := h.items[id]
	return it, ok
}

func (h *Hub) views() []View {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]View, 0, len(h.order))
	for _, id := range h.order {
		out = append(out, h.items[id].view())
	}
	return out
}

func (h *Hub) services() []Service {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Service, 0, len(h.order))
	for _, id := range h.order {
		out = append(out, h.items[id].svc())
	}
	return out
}

func (it item) svc() Service {
	if it.runner != nil {
		return it.runner.svc
	}
	return it.checker.svc
}

// closeAndWait stops the service for good. A process gets its kill time before the next one starts, so the
// new version doesn't fight the old one for a port.
func (it item) closeAndWait() {
	if it.runner != nil {
		it.runner.Close()
		it.runner.Stopped(12 * time.Second)
		return
	}
	it.checker.Close()
}

func (it item) wantsRun() bool { return it.runner != nil && it.runner.View().Want }

type editError struct {
	code int
	msg  string
}

func (e editError) Error() string { return e.msg }

func (h *Hub) create(s Service) error {
	h.editMu.Lock()
	defer h.editMu.Unlock()
	s = normalize(s)
	next := append(h.services(), s)
	if err := validate(next); err != nil {
		return editError{400, err.Error()}
	}
	if err := h.edit.write(next); err != nil {
		return h.writeErr(err)
	}
	it := h.edit.spawn(s, nil, false, h.edit.autostart)
	h.mu.Lock()
	h.order = append(h.order, s.ID)
	h.items[s.ID] = it
	h.mu.Unlock()
	return nil
}

func (h *Hub) update(id string, s Service) error {
	h.editMu.Lock()
	defer h.editMu.Unlock()
	old, ok := h.get(id)
	if !ok {
		return editError{404, "unknown service"}
	}
	if given := strings.TrimSpace(s.ID); given != "" && given != id {
		return editError{400, "the id of a service can't change"}
	}
	s.ID = id
	s = normalize(s)
	next := h.services()
	for i := range next {
		if next[i].ID == id {
			next[i] = s
		}
	}
	if err := validate(next); err != nil {
		return editError{400, err.Error()}
	}
	if err := h.edit.write(next); err != nil {
		return h.writeErr(err)
	}
	was := old.view()
	run := old.wantsRun() || (old.runner == nil && h.edit.autostart) // a check turned into a process starts like a new one
	logs := old.logs()
	old.closeAndWait()
	logs.Note("settings saved")
	it := h.edit.spawn(s, logs, was.Disabled, run)
	h.mu.Lock()
	h.items[id] = it
	h.mu.Unlock()
	return nil
}

func (h *Hub) remove(id string) error {
	h.editMu.Lock()
	defer h.editMu.Unlock()
	old, ok := h.get(id)
	if !ok {
		return editError{404, "unknown service"}
	}
	var next []Service
	for _, s := range h.services() {
		if s.ID != id {
			next = append(next, s)
		}
	}
	if err := h.edit.write(next); err != nil {
		return h.writeErr(err)
	}
	old.closeAndWait()
	h.mu.Lock()
	delete(h.items, id)
	for i, v := range h.order {
		if v == id {
			h.order = append(h.order[:i:i], h.order[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
	if h.onChange != nil {
		h.onChange() // drops the id from the saved disabled list
	}
	return nil
}

func (h *Hub) writeErr(err error) error {
	if errors.Is(err, errChanged) {
		return editError{409, err.Error()}
	}
	return editError{500, "could not write the config: " + err.Error()}
}

// ---- request checks ----

// allowedRequest reports whether a request may use the editing routes: the page's own header, a Host the
// hub knows, and no foreign Origin. The Host check is what stops DNS rebinding, where a hostile page is
// "same origin" with the hub and could otherwise send the header.
func (h *Hub) allowedRequest(r *http.Request) bool {
	if r.Header.Get("X-Ostler") != "1" {
		return false
	}
	host := strings.ToLower(r.Host)
	if hn, _, err := net.SplitHostPort(host); err == nil {
		host = hn
	}
	host = strings.Trim(host, "[]")
	if !h.edit.hosts[host] {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return true
}

func (h *Hub) editRoute(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.edit == nil {
			jsonOut(w, http.StatusForbidden, map[string]any{"ok": false, "error": "editing is turned off"})
			return
		}
		if !h.allowedRequest(r) {
			jsonOut(w, http.StatusForbidden, map[string]any{"ok": false, "error": "forbidden"})
			return
		}
		next(w, r)
	}
}

func readService(r *http.Request) (Service, error) {
	var s Service
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, editError{400, "invalid request: " + err.Error()}
	}
	return s, nil
}

func replyEdit(w http.ResponseWriter, err error, extra map[string]any) {
	if err == nil {
		out := map[string]any{"ok": true}
		for k, v := range extra {
			out[k] = v
		}
		jsonOut(w, 200, out)
		return
	}
	code := 500
	var ee editError
	if errors.As(err, &ee) {
		code = ee.code
	}
	jsonOut(w, code, map[string]any{"ok": false, "error": err.Error()})
}

func (h *Hub) manageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/meta", func(w http.ResponseWriter, r *http.Request) {
		meta := map[string]any{"canEdit": h.edit != nil, "urls": h.urls}
		if h.edit != nil {
			meta["root"] = h.edit.root
		}
		jsonOut(w, 200, meta)
	})
	mux.HandleFunc("GET /api/services/{id}/config", h.editRoute(func(w http.ResponseWriter, r *http.Request) {
		it, ok := h.get(r.PathValue("id"))
		if !ok {
			jsonOut(w, 404, map[string]any{"ok": false, "error": "unknown service"})
			return
		}
		jsonOut(w, 200, it.svc())
	}))
	mux.HandleFunc("POST /api/services", h.editRoute(func(w http.ResponseWriter, r *http.Request) {
		s, err := readService(r)
		if err == nil {
			err = h.create(s)
		}
		replyEdit(w, err, map[string]any{"id": strings.TrimSpace(s.ID)})
	}))
	mux.HandleFunc("PUT /api/services/{id}", h.editRoute(func(w http.ResponseWriter, r *http.Request) {
		s, err := readService(r)
		if err == nil {
			err = h.update(r.PathValue("id"), s)
		}
		replyEdit(w, err, nil)
	}))
	mux.HandleFunc("DELETE /api/services/{id}", h.editRoute(func(w http.ResponseWriter, r *http.Request) {
		replyEdit(w, h.remove(r.PathValue("id")), nil)
	}))
}

// loopbackOnly reports whether every address in the bind list can only be reached from this machine.
func loopbackOnly(binds []string) bool {
	for _, b := range binds {
		b = strings.Trim(strings.TrimSpace(b), "[]")
		if b == "localhost" {
			continue
		}
		if ip := net.ParseIP(b); ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}
