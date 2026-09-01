package runtime

import (
	"context"

	"agent-runtime/pkg/api"
)

// Facade is the process-management surface the MCP server (and REPL) depend on.
// *Runtime implements it directly (the default, session-scoped path); the
// daemon's Client implements it by forwarding pkg/api requests over a Unix
// socket, so the same thin MCP handlers run against either a local runtime or a
// long-lived daemon without any transport logic in the handlers (Phase 3).
//
// Every method is context-free except the blocking waiters, which take a
// context so a stuck daemon can never hang the agent beyond the timeout.
type Facade interface {
	Start(ctx context.Context, req api.StartRequest) (*api.StartResult, error)
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	SetRestartPolicy(req api.SetRestartPolicyRequest) (*api.SetRestartPolicyResult, error)
	Status(id string) (*api.StatusResult, error)
	List() (*api.ListResult, error)
	GetLogs(req api.GetLogsRequest) (*api.GetLogsResult, error)
	ClearLogs(id, stream string) (*api.ClearLogsResult, error)
	SendStdin(id, data string) (*api.SendStdinResult, error)
	RemoveProcess(id string, force bool) (*api.RemoveProcessResult, error)
	WaitForLog(ctx context.Context, req api.WaitForLogRequest) (*api.WaitForLogResult, error)
	WaitForExit(ctx context.Context, req api.WaitForExitParams) (*api.WaitForExitResult, error)
	Apps() (*api.ListAppsResult, error)
	SignalProcess(ctx context.Context, req api.SignalProcessRequest) (*api.SignalProcessResult, error)
	ProcessEnv(req api.ProcessEnvRequest) (*api.ProcessEnvResult, error)
	OpenShell(ctx context.Context, req api.OpenShellRequest) (*api.StartResult, error)
	SubscribeEvents(client string, req api.SubscribeEventsRequest) (*api.SubscribeEventsResult, error)
	GetEvents(req api.GetEventsRequest) (*api.GetEventsResult, error)
	UnsubscribeEvents(client, subscriptionID string) (*api.UnsubscribeEventsResult, error)
	RuntimeStats() (*api.RuntimeStats, error)
	GetAuditLog(lines int, contains string) (*api.AuditLogResult, error)
}

// compile-time check that *Runtime satisfies Facade.
var _ Facade = (*Runtime)(nil)
