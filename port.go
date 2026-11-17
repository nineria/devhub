package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Blocker is another process that holds the port a service needs. Without a check the service would
// start, find the port taken, quit, and be retried forever.
type Blocker struct {
	Port int    `json:"port"`
	Pid  int    `json:"pid"`
	Cmd  string `json:"cmd"`
}

// portHolder finds the process listening on a TCP port, using lsof. It returns nil when the port is free,
// and also when lsof isn't installed, so the check never blocks a service by mistake.
func portHolder(port int) *Blocker {
	if port <= 0 {
		return nil
	}
	out, err := exec.Command("lsof", "-nP", "-t", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN").Output()
	if err != nil {
		return nil
	}
	for _, f := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(f)
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmd, _ := exec.Command("ps", "-o", "command=", "-p", f).Output()
		return &Blocker{Port: port, Pid: pid, Cmd: trim(strings.TrimSpace(string(cmd)), 160)}
	}
	return nil
}

// waitPortFree holds the service back while something else is on its port, and checks again every few
// seconds, so it starts by itself once the port is free. It reports false if the service was stopped or
// closed while waiting.
func (r *Runner) waitPortFree() bool {
	port := r.svc.Port
	noted := 0
	for {
		b := portHolder(port)
		r.mu.Lock()
		r.blocker = b
		want := r.want
		if b != nil && want {
			r.state, r.note = "blocked", fmt.Sprintf("Port %d is in use by pid %d.", port, b.Pid)
		}
		r.mu.Unlock()
		if !want || r.closed() {
			return false
		}
		if b == nil {
			return true
		}
		if noted != b.Pid {
			noted = b.Pid
			r.log.Note("port %d is in use by pid %d (%s); waiting for it to be free", port, b.Pid, b.Cmd)
		}
		t := time.NewTimer(3 * time.Second)
		select {
		case <-t.C:
		case <-r.wake:
		case <-r.ctx.Done():
		}
		t.Stop()
	}
}

// FreePort stops the process holding this service's port: SIGTERM, then SIGKILL after 5 seconds. It only
// ever touches the process listening on the one port set in the config.
func (r *Runner) FreePort() error {
	if r.svc.Port <= 0 {
		return errors.New("this service has no port set")
	}
	b := portHolder(r.svc.Port)
	if b == nil {
		return nil
	}
	r.log.Note("stopping pid %d (%s), which is using port %d", b.Pid, b.Cmd, b.Port)
	_ = syscall.Kill(b.Pid, syscall.SIGTERM)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if portHolder(b.Port) == nil {
			r.kick()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(b.Pid, syscall.SIGKILL)
	time.Sleep(300 * time.Millisecond)
	if portHolder(b.Port) != nil {
		return fmt.Errorf("port %d is still in use", b.Port)
	}
	r.kick()
	return nil
}
