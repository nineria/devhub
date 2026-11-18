package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestLogBufKeepsOnlyTheLastLines(t *testing.T) {
	l := newLogBuf("t", nil)
	for i := 0; i < maxLogLines+50; i++ {
		l.Add("run", fmt.Sprint(i))
	}
	got := l.Snapshot()
	if len(got) != maxLogLines {
		t.Fatalf("kept %d lines, want %d", len(got), maxLogLines)
	}
	if got[0].M != "50" || got[len(got)-1].M != strconv.Itoa(maxLogLines+49) {
		t.Fatalf("wrong window: first %q last %q", got[0].M, got[len(got)-1].M)
	}
}

func TestLogBufSubscriberGetsBacklogAndNewLines(t *testing.T) {
	l := newLogBuf("t", nil)
	l.Add("run", "old")
	backlog, ch, cancel := l.Subscribe()
	defer cancel()
	l.Add("run", "new")
	if len(backlog) != 1 || backlog[0].M != "old" {
		t.Fatalf("backlog %v", backlog)
	}
	select {
	case line := <-ch:
		if line.M != "new" {
			t.Fatalf("got %q", line.M)
		}
	case <-time.After(time.Second):
		t.Fatal("no live line")
	}
}

func TestFingerprintSeesChangesAndIgnoresNodeModules(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1"), 0o644)
	n1, m1, s1 := fingerprint([]string{dir})
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "x.js"), []byte("noise"), 0o644)
	n2, m2, s2 := fingerprint([]string{dir})
	if n1 != n2 || m1 != m2 || s1 != s2 {
		t.Fatal("node_modules should not count")
	}
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("12"), 0o644)
	n3, m3, s3 := fingerprint([]string{dir})
	if n3 == n1 && m3 == m1 && s3 == s1 {
		t.Fatal("an edit should change the fingerprint")
	}
}

func testRunner(t *testing.T, svc Service) *Runner {
	t.Helper()
	svc.ID, svc.Name, svc.Kind = "t", "t", "process"
	r := newRunner(svc, t.TempDir(), childEnv())
	r.Run()
	t.Cleanup(r.Stop)
	return r
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestStopKillsTheWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// The shell starts a child, like start-server.sh starting a server; both must die on Stop.
	r := testRunner(t, Service{Run: fmt.Sprintf("sleep 300 & echo $! > %s; wait", pidFile)})
	r.Start()
	waitFor(t, "running", 5*time.Second, func() bool { return r.View().State == "running" })
	waitFor(t, "child pid", 5*time.Second, func() bool { _, err := os.Stat(pidFile); return err == nil })
	raw, _ := os.ReadFile(pidFile)
	child, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if !alive(child) {
		t.Fatal("child should be running")
	}
	r.Stop()
	waitFor(t, "stopped", 12*time.Second, func() bool { return r.View().State == "stopped" })
	waitFor(t, "child gone", 5*time.Second, func() bool { return !alive(child) })
}

func TestACrashIsRestartedAndExitZeroCountsAsACrash(t *testing.T) {
	r := testRunner(t, Service{Run: "exit 0"})
	r.Start()
	waitFor(t, "a restart", 6*time.Second, func() bool { return r.View().Restarts >= 1 })
	if st := r.View().State; st == "stopped" || st == "running" {
		t.Fatalf("state %q after a self-exit", st)
	}
}

func TestFailedBuildKeepsThePreviousVersionRunning(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "break")
	svc := Service{Dir: "", Build: fmt.Sprintf("[ ! -e %s ]", flag), Run: "sleep 300"}
	r := testRunner(t, svc)
	r.Start()
	waitFor(t, "running", 8*time.Second, func() bool { return r.View().State == "running" })
	pid := r.View().Pid

	os.WriteFile(flag, nil, 0o644) // the next build fails
	r.Restart()
	waitFor(t, "build failure noted", 8*time.Second, func() bool { return strings.Contains(r.View().Note, "Build failed") })
	if v := r.View(); v.State != "running" || v.Pid != pid || !alive(pid) {
		t.Fatalf("old version should keep running: %+v", v)
	}

	os.Remove(flag) // now the build passes and the process is replaced
	r.Restart()
	waitFor(t, "new process", 10*time.Second, func() bool { v := r.View(); return v.State == "running" && v.Pid != pid && v.Pid != 0 })
}

func TestRestartWhenStoppedStartsIt(t *testing.T) {
	r := testRunner(t, Service{Run: "sleep 300"})
	r.Restart()
	waitFor(t, "running", 8*time.Second, func() bool { return r.View().State == "running" })
}

func TestCheckerStateHeartbeatFreeAndButtons(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok")
	svc := Service{ID: "c", Name: "c", Kind: "check", Interval: 1,
		Check:   fmt.Sprintf("[ -e %s ]", ok),
		Buttons: []Button{{ID: "go", Label: "Go", Cmd: fmt.Sprintf("touch %s && echo done", ok)}}}
	c := newChecker(svc, dir, childEnv())
	c.Run()
	waitFor(t, "failing", 5*time.Second, func() bool { return c.View().State == "failing" })
	out, err := c.RunButton("go")
	if err != nil || out != "done" {
		t.Fatalf("button: %q %v", out, err)
	}
	waitFor(t, "ok after the button", 5*time.Second, func() bool { return c.View().State == "ok" })
	if _, err := c.RunButton("missing"); err == nil {
		t.Fatal("an unknown button must be refused")
	}
}

func TestConfigRejectsBadServices(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	os.WriteFile(p, []byte(`{"root":"/tmp","services":[{"id":"a","kind":"process"}]}`), 0o644)
	if _, err := loadConfig(p); err == nil {
		t.Fatal("a process without run must be rejected")
	}
	os.WriteFile(p, []byte(`{"root":"/tmp","services":[{"id":"a","kind":"check","check":"true"},{"id":"a","kind":"check","check":"true"}]}`), 0o644)
	if _, err := loadConfig(p); err == nil {
		t.Fatal("a repeated id must be rejected")
	}
}

func TestViewsNeverSendNullLists(t *testing.T) {
	c := newChecker(Service{ID: "c", Name: "c", Kind: "check", Check: "true"}, t.TempDir(), childEnv())
	r := newRunner(Service{ID: "r", Name: "r", Kind: "process", Run: "true"}, t.TempDir(), childEnv())
	for _, v := range []View{c.View(), r.View()} {
		if v.Links == nil || v.Buttons == nil {
			t.Fatalf("%s: links or buttons are nil, which the page receives as null", v.ID)
		}
	}
}

func TestLogLevelsCountsAndTheCombinedLog(t *testing.T) {
	allLogs.Clear()
	l := newLogBuf("svc", allLogs)
	l.Add("build", "compiling")
	l.Add("run", "ERROR: it broke")
	l.Add("run", "warning: deprecated call")
	lines := l.Snapshot()
	if lines[0].L != "info" || lines[1].L != "error" || lines[2].L != "warn" {
		t.Fatalf("levels: %+v", lines)
	}
	if e, w := l.Counts(); e != 1 || w != 1 {
		t.Fatalf("counts %d errors %d warnings", e, w)
	}
	l.ResetCounts()
	if e, w := l.Counts(); e != 0 || w != 0 {
		t.Fatal("counts should reset")
	}
	all := allLogs.Snapshot()
	if len(all) != 3 || all[1].N != "svc" || all[0].S != "build" {
		t.Fatalf("combined log: %+v", all)
	}
	l.Clear()
	if len(l.Snapshot()) != 0 {
		t.Fatal("clear should empty the log")
	}
}

func TestDisabledServiceDoesNotStartAndStaysDisabledInTheView(t *testing.T) {
	r := testRunner(t, Service{Run: "sleep 300"})
	r.Disable()
	r.Start()
	time.Sleep(300 * time.Millisecond)
	if v := r.View(); v.State != "disabled" || v.Pid != 0 || !v.Disabled {
		t.Fatalf("a disabled service must not run: %+v", v)
	}
	r.Enable()
	waitFor(t, "running after enable", 8*time.Second, func() bool { return r.View().State == "running" })
}

func TestBuildResultIsRecorded(t *testing.T) {
	r := testRunner(t, Service{Build: "true", Run: "sleep 300"})
	r.Start()
	waitFor(t, "running", 8*time.Second, func() bool { return r.View().State == "running" })
	b := r.View().Build
	if b == nil || !b.OK || b.At == 0 {
		t.Fatalf("build info: %+v", b)
	}
}

func TestDisabledChoiceIsSavedAndLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	c := newChecker(Service{ID: "c", Name: "c", Kind: "check", Check: "true"}, t.TempDir(), childEnv())
	c.Disable()
	hub := &Hub{order: []string{"c"}, items: map[string]item{"c": {checker: c}}}
	saveDisabled(path, hub)
	if !loadDisabled(path)["c"] {
		t.Fatal("the disabled service should be loaded back")
	}
}

func TestZeroCountersAreNotErrors(t *testing.T) {
	cases := map[string]string{
		"Monitored group=websites; success=true; errors=0; duration=3ms": "info",
		"en1 v4 up  link down  failed 0":                                 "info",
		"0 errors, 0 warnings":                                           "info",
		"Monitored success=false; errors=1; duration=0s":                 "error",
		"panic: runtime error":                                           "error",
		"warning: deprecated call":                                       "warn",
		"nothing special":                                                "info",
	}
	for msg, want := range cases {
		if got := levelOf(msg); got != want {
			t.Errorf("%q: got %s, want %s", msg, got, want)
		}
	}
}

// testHub builds a hub that can edit a config file in a temp dir.
func testHub(t *testing.T, services ...Service) (*Hub, *editor) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "devhub.json")
	cfg := &Config{Root: dir, RawRoot: dir}
	e := newEditor(path, cfg, childEnv(), true, nil)
	if err := e.write(services); err != nil {
		t.Fatal(err)
	}
	h := &Hub{items: map[string]item{}, edit: e}
	for _, s := range services {
		h.order = append(h.order, s.ID)
		h.items[s.ID] = e.spawn(s, nil, false, true)
	}
	t.Cleanup(func() {
		for _, it := range h.items {
			it.closeAndWait()
		}
	})
	return h, e
}

func TestEditCreateUpdateDeleteWritesTheFileAndSwapsTheService(t *testing.T) {
	h, e := testHub(t)
	if err := h.create(Service{ID: "a", Kind: "process", Run: "sleep 60", Buttons: []Button{{Label: "Go now", Cmd: "true"}}}); err != nil {
		t.Fatal(err)
	}
	r := h.items["a"].runner
	waitFor(t, "running", 5*time.Second, func() bool { return r.View().State == "running" })
	pid := r.View().Pid

	if err := h.create(Service{ID: "a", Kind: "check", Check: "true"}); err == nil {
		t.Fatal("a repeated id must be refused")
	}
	if err := h.create(Service{ID: "b c", Kind: "check", Check: "true"}); err == nil {
		t.Fatal("an id with a space must be refused")
	}

	// Update: the old process dies, a new one starts, and the log history is kept.
	if err := h.update("a", Service{Name: "Renamed", Kind: "process", Run: "sleep 61"}); err != nil {
		t.Fatal(err)
	}
	if alive(pid) {
		t.Fatal("the old process should be gone after an update")
	}
	r2 := h.items["a"].runner
	waitFor(t, "running again", 5*time.Second, func() bool { return r2.View().State == "running" })
	if r2.View().Name != "Renamed" || r2.Logs() != r.Logs() {
		t.Fatal("the update should apply and keep the log")
	}
	if err := h.update("a", Service{ID: "other", Kind: "process", Run: "true"}); err == nil {
		t.Fatal("the id must not change")
	}

	got, err := loadConfig(e.path)
	if err != nil || len(got.Services) != 1 || got.Services[0].Name != "Renamed" || got.Services[0].Run != "sleep 61" {
		t.Fatalf("file: %+v %v", got, err)
	}

	pid2 := r2.View().Pid
	if err := h.remove("a"); err != nil {
		t.Fatal(err)
	}
	if alive(pid2) || len(h.views()) != 0 {
		t.Fatal("delete should stop the process and drop the service")
	}
	select {
	case <-r2.ctx.Done():
	default:
		t.Fatal("the deleted runner's loops should be told to stop")
	}
	if got, _ := loadConfig(e.path); len(got.Services) != 0 {
		t.Fatalf("file still has %d services", len(got.Services))
	}
}

func TestEditKeepsDisabledAndRefusesAfterAHandEdit(t *testing.T) {
	h, e := testHub(t, Service{ID: "c", Name: "c", Kind: "check", Check: "true", Interval: 1})
	h.items["c"].disable()
	if err := h.update("c", Service{Kind: "check", Check: "true", Interval: 5}); err != nil {
		t.Fatal(err)
	}
	if !h.items["c"].view().Disabled {
		t.Fatal("a disabled service stays disabled after an edit")
	}
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(e.path, []byte(`{"root":"/tmp","services":[]}`), 0o644)
	err := h.update("c", Service{Kind: "check", Check: "true"})
	var ee editError
	if !errors.As(err, &ee) || ee.code != 409 {
		t.Fatalf("a hand edit should give a 409, got %v", err)
	}
}

func TestEditRoutesNeedHeaderHostAndOrigin(t *testing.T) {
	h, _ := testHub(t, Service{ID: "c", Name: "c", Kind: "check", Check: "true"})
	srv := httptest.NewServer(h.routes(os.DirFS(t.TempDir())))
	defer srv.Close()
	do := func(method, path, body string, hdr map[string]string) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		if host := hdr["Host"]; host != "" {
			req.Host = host
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	ok := map[string]string{"X-Devhub": "1"}
	body := `{"id":"n","kind":"check","check":"true"}`
	if c := do("POST", "/api/services", body, nil); c != 403 {
		t.Fatalf("no header: %d", c)
	}
	if c := do("POST", "/api/services", body, map[string]string{"X-Devhub": "1", "Host": "evil.example:8090"}); c != 403 {
		t.Fatalf("foreign host (DNS rebinding): %d", c)
	}
	if c := do("POST", "/api/services", body, map[string]string{"X-Devhub": "1", "Origin": "http://evil.example"}); c != 403 {
		t.Fatalf("foreign origin: %d", c)
	}
	if c := do("GET", "/api/services/c/config", "", nil); c != 403 {
		t.Fatalf("reading a config without the header: %d", c)
	}
	if c := do("POST", "/api/services", body, ok); c != 200 {
		t.Fatalf("a good request: %d", c)
	}
	if c := do("POST", "/api/services", `{"id":"m","kind":"check","check":"true","bogus":1}`, ok); c != 400 {
		t.Fatalf("an unknown field: %d", c)
	}
	if c := do("DELETE", "/api/services/n", "", ok); c != 200 {
		t.Fatalf("delete: %d", c)
	}
	h.edit = nil
	if c := do("POST", "/api/services", body, ok); c != 403 {
		t.Fatalf("editing off: %d", c)
	}
}

func TestLoopbackOnlyAndNormalize(t *testing.T) {
	if !loopbackOnly([]string{"127.0.0.1", " localhost", "::1"}) || loopbackOnly([]string{"127.0.0.1", "100.64.0.12"}) || loopbackOnly([]string{"0.0.0.0"}) {
		t.Fatal("loopbackOnly")
	}
	s := normalize(Service{ID: " x ", Kind: "task", Check: " c ", Interval: 9, Heartbeat: &Heartbeat{URL: "u"},
		Run: "r", Watch: []string{" ", "a"},
		Buttons: []Button{{Label: "Do it", Cmd: "true"}, {Label: "Do it", Cmd: "true"}, {}}})
	if s.ID != "x" || s.Name != "x" || s.Run != "" || s.Interval != 0 || s.Heartbeat != nil || len(s.Buttons) != 2 ||
		s.Buttons[0].ID != "do-it" || s.Buttons[1].ID != "do-it-2" {
		t.Fatalf("normalize: %+v", s)
	}
}

// listener starts a process that listens on a free port, and returns the port.
func listener(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cmd := exec.Command("python3", "-c", fmt.Sprintf(
		"import socket,time\ns=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);s.bind(('127.0.0.1',%d));s.listen();time.sleep(60)", port))
	if err := cmd.Start(); err != nil {
		t.Skip("python3 not available")
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitFor(t, "the listener", 5*time.Second, func() bool { return portHolder(port) != nil })
	return cmd, port
}

func TestABusyPortBlocksTheServiceInsteadOfCrashLooping(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not available")
	}
	cmd, port := listener(t)
	r := testRunner(t, Service{Run: "sleep 60", Port: port})
	r.Start()
	waitFor(t, "blocked", 5*time.Second, func() bool { return r.View().State == "blocked" })
	v := r.View()
	if v.Blocker == nil || v.Blocker.Pid != cmd.Process.Pid || v.Pid != 0 || v.Restarts != 0 {
		t.Fatalf("view: %+v blocker %+v", v, v.Blocker)
	}
	if err := r.FreePort(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running after the port is freed", 8*time.Second, func() bool { return r.View().State == "running" })
	if r.View().Blocker != nil {
		t.Fatal("the blocker should be cleared")
	}
	r.Stop()
}

func TestBlockedServiceCanBeStopped(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not available")
	}
	_, port := listener(t)
	r := testRunner(t, Service{Run: "sleep 60", Port: port})
	r.Start()
	waitFor(t, "blocked", 5*time.Second, func() bool { return r.View().State == "blocked" })
	r.Stop()
	waitFor(t, "stopped", 5*time.Second, func() bool { v := r.View(); return v.State == "stopped" && v.Blocker == nil })
}

func TestCountsFollowTheLinesStillInTheLog(t *testing.T) {
	l := newLogBuf("t", nil)
	l.Add("run", "panic: boom")
	l.Add("run", "warning: slow")
	for i := 0; i < maxLogLines; i++ {
		l.Add("run", "fine")
	}
	if e, w := l.Counts(); e != 0 || w != 0 {
		t.Fatalf("the error and the warning left the log, but the counts are %d and %d", e, w)
	}
}

func TestCountsStayRightAfterAResetAndOldLinesLeaving(t *testing.T) {
	l := newLogBuf("t", nil)
	l.Add("run", "panic: old")
	l.ResetCounts() // a restart: the old error no longer counts
	l.Add("run", "panic: new")
	for i := 0; i < maxLogLines-2; i++ {
		l.Add("run", "fine")
	}
	l.Add("run", "fine") // pushes "old" out of the log
	if e, _ := l.Counts(); e != 1 {
		t.Fatalf("errors %d, want 1 (the new one)", e)
	}
	l.Add("run", "fine") // pushes "new" out
	if e, _ := l.Counts(); e != 0 {
		t.Fatalf("errors %d, want 0", e)
	}
}
