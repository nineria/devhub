package main

import (
	"fmt"
	"os"
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
