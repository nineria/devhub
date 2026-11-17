package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxLogLines = 2000 // a fixed ring: the hub must stay small

// Line is one log line. S is where it came from (build, run, or hub for the hub's own notes) and L its
// level, guessed from the text: the services don't tell us.
type Line struct {
	T int64  `json:"t"` // unix milliseconds
	S string `json:"s"` // build, run, hub
	L string `json:"l"` // info, warn, error
	M string `json:"m"`
	N string `json:"n,omitempty"` // service id, set on the combined log
}

var (
	errWord  = regexp.MustCompile(`(?i)\b(error|errors|fatal|panic|panicked|exception|failed|failure)\b`)
	warnWord = regexp.MustCompile(`(?i)\b(warn|warning|warnings|deprecated)\b`)
)

// zeroCount matches "errors=0", "failed 0" and "0 errors": a counter at zero is good news, not an error.
var zeroCount = regexp.MustCompile(`(?i)\b(error|errors|failed|failures?|warnings?)\s*[=:]?\s*0\b|\b0\s+(error|errors|failures?|warnings?)\b`)

func levelOf(msg string) string {
	msg = zeroCount.ReplaceAllString(msg, "")
	switch {
	case errWord.MatchString(msg):
		return "error"
	case warnWord.MatchString(msg):
		return "warn"
	}
	return "info"
}

// LogBuf keeps the last maxLogLines lines of a service, counts its errors and warnings, and fans new lines
// out to live readers. A service's buffer also feeds the combined one.
type LogBuf struct {
	id     string
	max    int // lines kept
	parent *LogBuf
	mu     sync.Mutex
	lines  []Line
	subs   map[chan Line]struct{}
	errs   int
	warns  int
}

func newLogBuf(id string, parent *LogBuf) *LogBuf {
	return &LogBuf{id: id, max: maxLogLines, parent: parent, subs: map[chan Line]struct{}{}}
}

// allLogs is the combined log of every service.
var allLogs = func() *LogBuf {
	l := newLogBuf("", nil)
	l.max = maxLogLines * 2
	return l
}()

func (l *LogBuf) push(line Line) {
	l.mu.Lock()
	if len(l.lines) >= l.max {
		copy(l.lines, l.lines[len(l.lines)-l.max+1:])
		l.lines = l.lines[:l.max-1]
	}
	l.lines = append(l.lines, line)
	switch line.L {
	case "error":
		l.errs++
	case "warn":
		l.warns++
	}
	for ch := range l.subs {
		select {
		case ch <- line:
		default: // a slow reader loses lines instead of blocking the service
		}
	}
	l.mu.Unlock()
}

// Add records a line of program output from src ("build" or "run").
func (l *LogBuf) Add(src, msg string) {
	line := Line{T: time.Now().UnixMilli(), S: src, L: levelOf(msg), M: msg}
	l.push(line)
	if l.parent != nil {
		line.N = l.id
		l.parent.push(line)
	}
}

// Note adds a line from the hub itself (building, crashed, restarted).
func (l *LogBuf) Note(format string, a ...any) {
	l.Add("hub", fmt.Sprintf(format, a...))
}

// Subscribe returns the lines so far and a channel of new ones; call the cancel func when done.
func (l *LogBuf) Subscribe() ([]Line, chan Line, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	backlog := append([]Line(nil), l.lines...)
	ch := make(chan Line, 256)
	l.subs[ch] = struct{}{}
	return backlog, ch, func() {
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}
}

func (l *LogBuf) Snapshot() []Line {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Line(nil), l.lines...)
}

// Clear empties the log and the counts, like Tilt's Clear button.
func (l *LogBuf) Clear() {
	l.mu.Lock()
	l.lines, l.errs, l.warns = nil, 0, 0
	l.mu.Unlock()
}

// ResetCounts starts the error and warning counts again; called when a build or a run begins, so the
// counts always mean "since the last start".
func (l *LogBuf) ResetCounts() {
	l.mu.Lock()
	l.errs, l.warns = 0, 0
	l.mu.Unlock()
}

func (l *LogBuf) Counts() (errs, warns int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.errs, l.warns
}

func trim(s string, n int) string {
	if len(s) > n {
		return strings.TrimSpace(s[:n])
	}
	return s
}
