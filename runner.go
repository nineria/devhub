package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Runner supervises one long-running process: build, run in its own process group, restart after a
// crash with a growing delay, rebuild and restart when watched files change. A failed rebuild keeps the
// previous version running.
type Runner struct {
	svc  Service
	root string
	dir  string
	env  []string
	log  *LogBuf

	mu         sync.Mutex
	state      string // stopped, building, starting, running, backoff, failed
	ready      bool
	note       string
	since      time.Time
	restarts   int
	want       bool
	skipBuild  bool
	restartReq bool
	cmd        *exec.Cmd
	exited     chan struct{}
	wake       chan struct{}
	buildMu    sync.Mutex
	disabled   bool
	build      *BuildView // the last build, if there has been one
	blocker    *Blocker   // set while another process holds the service's port
	ctx        context.Context
	cancel     context.CancelFunc // Close: ends the loops and any build in progress
}

func newRunner(svc Service, root string, env []string) *Runner {
	dir := root
	if svc.Dir != "" {
		dir = filepath.Join(root, svc.Dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		svc: svc, root: root, dir: dir, env: env, log: newLogBuf(svc.ID, allLogs),
		state: "stopped", since: time.Now(), wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
	}
}

// Close stops the process and ends every loop for good, which a deleted or replaced service needs.
// Wait for it with Stopped.
func (r *Runner) Close() {
	r.mu.Lock()
	r.want = false
	r.mu.Unlock()
	r.cancel()
	r.killProc()
	r.kick()
}

// Stopped waits until the process is gone (the kill escalates after 8 seconds).
func (r *Runner) Stopped(limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if r.View().Pid == 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (r *Runner) closed() bool { return r.ctx.Err() != nil }

// nap sleeps, and reports false when the runner was closed meanwhile.
func (r *Runner) nap(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.ctx.Done():
		return false
	}
}

func (r *Runner) kick() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runner) setState(state, note string) {
	r.mu.Lock()
	if r.state != state {
		r.since = time.Now()
	}
	r.state, r.note = state, note
	r.mu.Unlock()
}

// Run starts the supervising loops. Nothing runs until Start is called.
func (r *Runner) Run() {
	go r.supervise()
	go r.probeLoop()
	if len(r.svc.Watch) > 0 {
		go r.watchLoop()
	}
}

func (r *Runner) Start() {
	r.mu.Lock()
	if r.disabled || r.closed() {
		r.mu.Unlock()
		return
	}
	r.want = true
	r.mu.Unlock()
	r.kick()
}

// Disable stops the service and keeps it from starting until it is enabled again.
func (r *Runner) Disable() {
	r.mu.Lock()
	r.disabled = true
	r.mu.Unlock()
	r.log.Note("disabled")
	r.Stop()
}

func (r *Runner) Enable() {
	r.mu.Lock()
	r.disabled = false
	r.mu.Unlock()
	r.log.Note("enabled")
	r.Start()
}

func (r *Runner) Stop() {
	r.mu.Lock()
	r.want = false
	r.mu.Unlock()
	r.killProc()
	r.kick()
}

// Restart rebuilds first; the running version is only replaced when the build succeeds.
func (r *Runner) Restart() {
	r.mu.Lock()
	want := r.want
	r.mu.Unlock()
	if !want {
		r.Start()
		return
	}
	go r.rebuildAndRestart()
}

func (r *Runner) rebuildAndRestart() {
	r.buildMu.Lock()
	defer r.buildMu.Unlock()
	if r.svc.Build != "" {
		r.mu.Lock()
		alive := r.cmd != nil
		r.mu.Unlock()
		if !alive {
			r.setState("building", "")
		}
		r.log.Note("building")
		if !r.runBuild() {
			if alive {
				r.setState(r.currentState(), "Build failed. Still running the previous version.")
			} else {
				r.setState("failed", "Build failed")
			}
			return
		}
	}
	r.mu.Lock()
	r.note = ""
	r.skipBuild = true
	r.restartReq = r.cmd != nil
	r.mu.Unlock()
	r.log.Note("restarting")
	r.killProc()
	r.kick()
}

func (r *Runner) currentState() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *Runner) supervise() {
	backoff := time.Second
	for {
		if !r.waitWant() {
			return
		}
		r.mu.Lock()
		skip := r.skipBuild
		r.skipBuild = false
		r.mu.Unlock()

		if r.svc.Build != "" && !skip {
			r.setState("building", "")
			r.log.Note("building")
			r.buildMu.Lock()
			ok := r.runBuild()
			r.buildMu.Unlock()
			if !ok {
				r.setState("failed", "Build failed")
				r.sleep(backoff)
				backoff = nextBackoff(backoff)
				continue
			}
		}
		r.mu.Lock()
		if !r.want {
			r.mu.Unlock()
			continue
		}
		r.mu.Unlock()

		if !r.waitPortFree() {
			continue
		}
		started := time.Now()
		code := r.runProc()
		ranFor := time.Since(started)

		r.mu.Lock()
		req, want := r.restartReq, r.want
		r.restartReq = false
		r.mu.Unlock()
		if !want {
			continue
		}
		if req {
			backoff = time.Second
			r.mu.Lock()
			r.restarts++
			r.mu.Unlock()
			continue
		}
		// An exit with code 0 is a failure too: a service that quits on its own is not "up".
		r.log.Note("exited (%s) after %s; trying again in %s", code, ranFor.Round(time.Second), backoff)
		r.setState("backoff", "Stopped by itself. Trying again.")
		r.mu.Lock()
		r.skipBuild = true
		r.restarts++
		r.mu.Unlock()
		r.sleep(backoff)
		if ranFor > 30*time.Second {
			backoff = time.Second
		} else {
			backoff = nextBackoff(backoff)
		}
	}
}

func nextBackoff(d time.Duration) time.Duration {
	if d *= 2; d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

func (r *Runner) waitWant() bool {
	for {
		if r.closed() {
			return false
		}
		r.mu.Lock()
		if r.want {
			r.mu.Unlock()
			return true
		}
		if r.state != "stopped" {
			r.since = time.Now()
		}
		r.state, r.ready, r.note, r.blocker = "stopped", false, "", nil
		r.mu.Unlock()
		select {
		case <-r.wake:
		case <-r.ctx.Done():
			return false
		}
	}
}

func (r *Runner) sleep(d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-r.wake:
	case <-r.ctx.Done():
	}
}

func (r *Runner) runBuild() bool {
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Minute)
	defer cancel()
	started := time.Now()
	r.log.ResetCounts()
	ok := r.build1(ctx)
	r.mu.Lock()
	r.build = &BuildView{OK: ok, At: time.Now().Unix(), Ms: time.Since(started).Milliseconds()}
	r.mu.Unlock()
	return ok
}

func (r *Runner) build1(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "sh", "-c", r.svc.Build)
	cmd.Dir, cmd.Env = r.dir, r.env
	pr, pw, _ := os.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		r.log.Note("build could not start: %v", err)
		return false
	}
	pw.Close()
	done := make(chan struct{})
	go func() { r.pump(pr, "build"); close(done) }()
	err := cmd.Wait()
	<-done
	if err != nil {
		r.log.Note("build failed: %v", err)
		return false
	}
	r.log.Note("build ok")
	return true
}

func (r *Runner) runProc() string {
	cmd := exec.Command("sh", "-c", r.svc.Run)
	cmd.Dir, cmd.Env = r.dir, r.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pr, pw, _ := os.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		r.log.Note("could not start: %v", err)
		return "could not start"
	}
	pw.Close()
	go r.pump(pr, "run")

	exited := make(chan struct{})
	r.log.ResetCounts()
	r.mu.Lock()
	r.cmd, r.exited, r.ready = cmd, exited, r.svc.Ready == ""
	r.state, r.since = "starting", time.Now()
	if r.svc.Ready == "" {
		r.state = "running"
	}
	if r.note == "Stopped by itself. Trying again." {
		r.note = ""
	}
	r.mu.Unlock()
	r.log.Note("started (pid %d)", cmd.Process.Pid)

	err := cmd.Wait()
	close(exited)
	r.mu.Lock()
	r.cmd, r.ready = nil, false
	r.mu.Unlock()
	if err != nil {
		return err.Error()
	}
	return "code 0"
}

// killProc stops the whole process group, so a script's child servers go with it.
func (r *Runner) killProc() {
	r.mu.Lock()
	cmd, exited := r.cmd, r.exited
	r.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		select {
		case <-exited:
		case <-time.After(8 * time.Second):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}

func (r *Runner) pump(pr *os.File, src string) {
	defer pr.Close()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		r.log.Add(src, sc.Text())
	}
}

func probe(url string) bool {
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Get(url)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	res.Body.Close()
	return res.StatusCode < 400
}

// probeLoop marks the process ready once its URL answers. It polls quickly while starting and every 15s
// once ready.
func (r *Runner) probeLoop() {
	for {
		r.mu.Lock()
		alive, url := r.cmd != nil, r.svc.Ready
		r.mu.Unlock()
		wait := 2 * time.Second
		if alive && url != "" {
			ok := probe(url)
			r.mu.Lock()
			if r.cmd != nil {
				r.ready = ok
				if ok && r.state == "starting" {
					r.state, r.since = "running", time.Now()
				}
			}
			r.mu.Unlock()
			if ok {
				wait = 15 * time.Second
			}
		}
		if !r.nap(wait) {
			return
		}
	}
}

var ignoredDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "target": true, ".next": true,
	".run": true, "logs": true, "data": true, "tmp": true,
}

// fingerprint summarises the watched files. Polling avoids the file descriptor per file that kqueue
// watching costs, which node_modules would exhaust.
func fingerprint(paths []string) (n int, newest int64, size int64) {
	for _, p := range paths {
		_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != p && ignoredDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == ".DS_Store" {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			n++
			size += info.Size()
			if t := info.ModTime().UnixNano(); t > newest {
				newest = t
			}
			return nil
		})
	}
	return
}

func (r *Runner) watchPaths() []string {
	out := make([]string, 0, len(r.svc.Watch))
	for _, w := range r.svc.Watch {
		out = append(out, filepath.Join(r.root, w))
	}
	return out
}

func (r *Runner) watchLoop() {
	paths := r.watchPaths()
	n, newest, size := fingerprint(paths)
	for {
		if !r.nap(2 * time.Second) {
			return
		}
		n2, newest2, size2 := fingerprint(paths)
		if n2 == n && newest2 == newest && size2 == size {
			continue
		}
		// Wait until the files stop changing (an editor or a formatter often writes several).
		for {
			if !r.nap(700 * time.Millisecond) {
				return
			}
			n3, newest3, size3 := fingerprint(paths)
			if n3 == n2 && newest3 == newest2 && size3 == size2 {
				break
			}
			n2, newest2, size2 = n3, newest3, size3
		}
		n, newest, size = n2, newest2, size2
		r.mu.Lock()
		want := r.want
		r.mu.Unlock()
		if want {
			r.log.Note("files changed")
			go r.rebuildAndRestart()
		}
	}
}

func (r *Runner) View() View {
	r.mu.Lock()
	defer r.mu.Unlock()
	pid := 0
	if r.cmd != nil && r.cmd.Process != nil {
		pid = r.cmd.Process.Pid
	}
	errs, warns := r.log.Counts()
	state := r.state
	if r.disabled {
		state = "disabled"
	}
	return View{
		ID: r.svc.ID, Name: r.svc.Name, Group: r.svc.Group, Kind: r.svc.Kind, Desc: r.svc.Desc,
		State: state, Ready: r.ready, Note: r.note, Since: r.since.Unix(), Restarts: r.restarts,
		Want: r.want, Pid: pid, Disabled: r.disabled, Errors: errs, Warnings: warns, Build: r.build, Blocker: r.blocker,
		Buttons: viewButtons(r.svc.Buttons), Links: viewLinks(r.svc.Links),
	}
}

// viewLinks never returns nil, so the page always gets a list ("null" would break it).
func viewLinks(ls []Link) []Link {
	if ls == nil {
		return []Link{}
	}
	return ls
}

func viewButtons(bs []Button) []ButtonView {
	out := make([]ButtonView, 0, len(bs))
	for _, b := range bs {
		out = append(out, ButtonView{ID: b.ID, Label: b.Label, Confirm: b.Confirm})
	}
	return out
}

func (r *Runner) Logs() *LogBuf { return r.log }
