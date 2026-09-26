package server

import (
	"agent-runtime/internal/session"
)

func sessionKind(k string) session.SessionKind {
	switch session.SessionKind(k) {
	case session.SessionKindMCP, session.SessionKindCLI, session.SessionKindGUI,
		session.SessionKindTUI, session.SessionKindWeb:
		return session.SessionKind(k)
	}
	return session.SessionKindMCP
}

func sessionHarness(h string) session.Harness {
	switch session.Harness(h) {
	case session.HarnessClaudeCode, session.HarnessCodex, session.HarnessGemini,
		session.HarnessOpenCode, session.HarnessCursor, session.HarnessWindsurf,
		session.HarnessVSCodeCopilot, session.HarnessZed, session.HarnessCline:
		return session.Harness(h)
	}
	if h == "" {
		return session.HarnessUnknown
	}
	return session.Harness(h)
}

func sessionJSON(se *session.Session) map[string]any {
	out := map[string]any{
		"id": se.ID, "kind": string(se.Kind), "harness": string(se.Harness),
		"clientPid": se.ClientPID, "workspaceId": se.WorkspaceID,
		"startedAt": se.StartedAt.Unix(), "lastSeenAt": se.LastSeenAt.Unix(),
	}
	if se.ClosedAt != nil {
		out["closedAt"] = se.ClosedAt.Unix()
	}
	return out
}
