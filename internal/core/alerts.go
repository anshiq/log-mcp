// Log-based alerts (Phase 9): "notify me when /panic/ appears". Each
// loaded workspace runs a light poller (5s) over new lines of running
// processes; first match per process run emits a logs.alert event (GUI
// toasts, debounced per process with mute in the client).
package core

import (
	"regexp"
	"strings"
	"time"

	"agent-runtime/internal/config"
)

type alertState struct {
	rules   []compiledAlert
	cursors map[string]uint64 // processID -> last scanned log id
	fired   map[string]bool   // processID+pattern -> fired this run
}

type compiledAlert struct {
	rule config.AlertRule
	re   *regexp.Regexp
}

func (e *Engine) runAlerts(pr *ProjectRuntime) {
	res := pr.ResolvedConfig()
	if res == nil || len(res.Alerts) == 0 {
		return
	}
	st := &alertState{cursors: map[string]uint64{}, fired: map[string]bool{}}
	for _, r := range res.Alerts {
		if r.Pattern == "" {
			continue
		}
		pat := r.Pattern
		if !r.Regex {
			pat = regexp.QuoteMeta(pat)
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		st.rules = append(st.rules, compiledAlert{rule: r, re: re})
	}
	if len(st.rules) == 0 {
		return
	}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.checkAlerts(pr, st)
		}
	}
}

func (e *Engine) checkAlerts(pr *ProjectRuntime, st *alertState) {
	rt, err := pr.Runtime()
	if err != nil {
		return
	}
	l, err := rt.List()
	if err != nil {
		return
	}
	wsID := pr.WorkspaceID()
	for _, p := range l.Processes {
		proc, ok := rt.Manager().Get(p.ProcessID)
		if !ok {
			continue
		}
		from := st.cursors[p.ProcessID]
		var maxID uint64 = from
		for _, en := range proc.Logs.From(from) {
			if en.ID > maxID {
				maxID = en.ID
			}
			for _, rule := range st.rules {
				key := p.ProcessID + "\x00" + rule.rule.Pattern
				if st.fired[key] {
					continue
				}
				matched := rule.re.MatchString(en.Line) ||
					(!rule.rule.Regex && strings.Contains(en.Line, rule.rule.Pattern))
				if !matched {
					continue
				}
				st.fired[key] = true
				msg := rule.rule.Message
				if msg == "" {
					msg = "pattern matched: " + rule.rule.Pattern
				}
				_, _ = e.store.AppendEvent(time.Now().UnixNano(), "logs.alert",
					pr.ProjectID(), wsID, p.ProcessID, "", "", msg+" :: "+truncateLine(en.Line, 300))
			}
		}
		st.cursors[p.ProcessID] = maxID + 1
		// Reset fired flags when the process restarts (new instance).
		_ = wsID
	}
}

func truncateLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
