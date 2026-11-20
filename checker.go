package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Checker runs a command on a timer ("is the gateway up?") and optionally sends a heartbeat to another
// monitor when it passes. Kind "task" does not repeat; it runs once and when a button asks for it.
type Checker struct {
	svc    Service
	dir    string
	env    []string
	log    *LogBuf
	hbTok  string
	nudge  chan struct{}
	runBtn sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	state   string // unknown, ok, failing, info
	fails   int    // checks failed in a row
	detail  string
	since   time.Time
	last    time.Time
	busy    string // label of the button running, if any
	hbError bool
	off     bool
}

func newChecker(svc Service, root string, env []string) *Checker {
	dir := root
	if svc.Dir != "" {
		dir = filepath.Join(root, svc.Dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Checker{svc: svc, dir: dir, env: env, log: newLogBuf(svc.ID, allLogs),
		state: "unknown", since: time.Now(), nudge: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
	if svc.Heartbeat != nil && svc.Heartbeat.TokenEnv != "" {
		for _, kv := range env {
			if k, v, ok := strings.Cut(kv, "="); ok && k == svc.Heartbeat.TokenEnv {
				c.hbTok = v
			}
		}
	}
	return c
}

func (c *Checker) Run() { go c.loop() }

// Close ends the loop for good and cancels a check or button that is running.
func (c *Checker) Close() { c.cancel() }

func (c *Checker) Disable() {
	c.mu.Lock()
	c.off = true
	c.mu.Unlock()
	c.log.Note("disabled")
}

func (c *Checker) Enable() {
	c.mu.Lock()
	c.off = false
	c.mu.Unlock()
	c.log.Note("enabled")
	c.recheck()
}

func (c *Checker) disabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.off
}

func (c *Checker) loop() {
	for {
		if c.ctx.Err() != nil {
			return
		}
		if !c.disabled() {
			c.runCheck()
		}
		var wait <-chan time.Time
		var timer *time.Timer
		if d := c.wait(); d > 0 {
			timer = time.NewTimer(d)
			wait = timer.C
		}
		select {
		case <-wait:
		case <-c.nudge:
		case <-c.ctx.Done():
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// wait is the pause before the next check. While a check fails it backs off for a machine that is switched
// off now and then: failInterval after the first failure, slowInterval once the retry failed too. 0 means no repeat.
func (c *Checker) wait() time.Duration {
	if c.svc.Kind != "check" || c.svc.Interval <= 0 {
		return 0
	}
	secs := c.svc.Interval
	c.mu.Lock()
	fails := c.fails
	c.mu.Unlock()
	if fails >= 1 && c.svc.FailEvery > secs {
		secs = c.svc.FailEvery
	}
	if fails >= 2 && c.svc.SlowEvery > secs {
		secs = c.svc.SlowEvery
	}
	return time.Duration(secs) * time.Second
}

func (c *Checker) recheck() {
	select {
	case c.nudge <- struct{}{}:
	default:
	}
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 140 {
				l = l[:140]
			}
			return l
		}
	}
	return ""
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if len(l) > 140 {
				l = l[:140]
			}
			return l
		}
	}
	return ""
}

func (c *Checker) exec(cmdline string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(c.ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdline)
	cmd.Dir, cmd.Env = c.dir, c.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *Checker) runCheck() {
	out, err := c.exec(c.svc.Check, 25*time.Second)
	state := "failing"
	if c.svc.Kind == "task" {
		state = "info"
	} else if err == nil {
		state = "ok"
	}
	c.mu.Lock()
	changed := c.state != state
	if state == "failing" {
		c.fails++
	} else {
		c.fails = 0
	}
	first := c.last.IsZero()
	if changed {
		c.since = time.Now()
	}
	detail := lastLine(out)
	if c.svc.Kind == "task" {
		detail = firstLine(out) // a status command's summary comes first, its table after
	}
	c.state, c.detail, c.last = state, detail, time.Now()
	c.mu.Unlock()

	switch {
	case c.svc.Kind == "task":
		if first {
			c.log.Note("status: %s", firstLine(out))
		}
	case changed || first:
		if state == "ok" {
			c.log.Note("up")
		} else {
			c.log.Note("not responding")
		}
	}
	if state == "ok" && c.svc.Heartbeat != nil && c.svc.Heartbeat.URL != "" {
		c.heartbeat()
	}
}

// expandVars fills ${NAME} from the environment list.
func expandVars(s string, env []string) string {
	return os.Expand(s, func(name string) string {
		for _, kv := range env {
			if k, v, ok := strings.Cut(kv, "="); ok && k == name {
				return v
			}
		}
		return ""
	})
}

// heartbeat tells the other monitor the service is healthy; it alerts when the heartbeats stop.
func (c *Checker) heartbeat() {
	hb := c.svc.Heartbeat
	method := hb.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequest(method, expandVars(hb.URL, c.env), nil)
	if err != nil {
		c.log.Note("heartbeat url is not valid: %v", err)
		return
	}
	if c.hbTok != "" {
		req.Header.Set("Authorization", "Bearer "+c.hbTok)
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	failed := err != nil || res.StatusCode >= 300
	if err == nil {
		res.Body.Close()
	}
	c.mu.Lock()
	was := c.hbError
	c.hbError = failed
	c.mu.Unlock()
	if failed && !was {
		c.log.Note("could not send the heartbeat")
	} else if !failed && was {
		c.log.Note("heartbeat works again")
	}
}

var errBusy = errors.New("another action is already running")

// RunButton runs a button's command from the config and returns its last line of output.
func (c *Checker) RunButton(id string) (string, error) {
	var b *Button
	for i := range c.svc.Buttons {
		if c.svc.Buttons[i].ID == id {
			b = &c.svc.Buttons[i]
		}
	}
	if b == nil {
		return "", errors.New("unknown action")
	}
	if !c.runBtn.TryLock() {
		return "", errBusy
	}
	defer c.runBtn.Unlock()
	c.mu.Lock()
	c.busy = b.Label
	c.mu.Unlock()
	c.log.Note("%s", b.Label)
	out, err := c.exec(b.Cmd, 150*time.Second)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			c.log.Add("run", line)
		}
	}
	c.mu.Lock()
	c.busy = ""
	c.mu.Unlock()
	c.recheck()
	if err != nil {
		c.log.Note("%s failed: %v", b.Label, err)
		return lastLine(out), err
	}
	c.log.Note("%s done", b.Label)
	return lastLine(out), nil
}

func (c *Checker) View() View {
	errs, warns := c.log.Counts()
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state
	if c.off {
		state = "disabled"
	}
	return View{
		ID: c.svc.ID, Name: c.svc.Name, Group: c.svc.Group, Kind: c.svc.Kind, Desc: c.svc.Desc,
		State: state, Detail: c.detail, Since: c.since.Unix(), Busy: c.busy, Disabled: c.off,
		Errors: errs, Warnings: warns, Buttons: viewButtons(c.svc.Buttons), Links: viewLinks(c.svc.Links),
	}
}

func (c *Checker) Logs() *LogBuf { return c.log }
