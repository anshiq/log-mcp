// v3 additions to the MCP tool surface: workspace-scoped list_processes
// plus get_project_info, validate/plan/apply_config, search_logs and
// list_sessions. Existing tool names and shapes are frozen; these are
// purely additive (§5.6, Appendix B).
package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-runtime/pkg/api"
)

type listProcessesIn struct {
	Scope string `json:"scope,omitempty" jsonschema:"Scope: workspace (default, this session's project), project (all workspaces of the project) or all (every workspace on this machine)."`
}

type applyConfigIn struct {
	YAML         string `json:"yaml" jsonschema:"Full project YAML to apply (project layer). Get the current content via get_project_info configPath first."`
	BaseRevision int64  `json:"base_revision,omitempty" jsonschema:"Optimistic concurrency: the revision id the edit is based on (from plan_config). 0 disables the check."`
	Message      string `json:"message,omitempty" jsonschema:"Revision message recorded in config history."`
}

type searchLogsIn struct {
	Query      string   `json:"query" jsonschema:"FTS query, or regex when regex=true."`
	ProcessIDs []string `json:"process_ids,omitempty" jsonschema:"Process ids to search; empty searches the whole workspace."`
	Regex      bool     `json:"regex,omitempty" jsonschema:"Treat query as a regular expression."`
	MaxRows    int      `json:"max_rows,omitempty" jsonschema:"Maximum matches (default 200, cap 1000)."`
}

type resolveProposalIn struct {
	ProposalID string `json:"proposal_id,omitempty" jsonschema:"Proposal id from get_project_info pendingProposal. Empty uses the current pending proposal."`
	Action     string `json:"action" jsonschema:"approve or dismiss."`
}

// v3Tools is implemented by *Bridge. Embedded (session-scoped) runtimes
// return a descriptive error directing the agent to daemon mode.
type v3Tools interface {
	ProjectInfo(ctx context.Context) (map[string]any, error)
	ValidateConfig(ctx context.Context, yaml string) (map[string]any, error)
	PlanConfig(ctx context.Context, yaml string) (map[string]any, error)
	ApplyConfig(ctx context.Context, yaml string, baseRevision int64, message string) (map[string]any, error)
	ResolveProposal(ctx context.Context, proposalID, action string) (map[string]any, error)
	SearchLogs(ctx context.Context, query string, processIDs []string, regex bool, maxRows int) (map[string]any, error)
	ListSessions(ctx context.Context) ([]map[string]any, error)
}

func bridgeOf(h *handlers) (*Bridge, error) {
	b, ok := h.rt.(*Bridge)
	if !ok {
		return nil, errNeedsDaemon()
	}
	return b, nil
}

func errNeedsDaemon() error {
	return &daemonRequiredError{}
}

type daemonRequiredError struct{}

func (e *daemonRequiredError) Error() string {
	return "this tool requires the v3 daemon (run agent-runtime serve without --embedded, or set runtime.daemon: true)"
}

func registerV3Tools(server *mcp.Server, h *handlers) {
	mcp.AddTool(server,
		&mcp.Tool{Name: "get_project_info", Description: "Where this project lives in v3: project/workspace ids, config revision and pending proposals, and which other sessions (agents, GUI) are attached here. Call it before editing config."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
			h.log(ctx, "get_project_info", nil)
			b, err := bridgeOf(h)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ProjectInfo(ctx)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "validate_config", Description: "Validate project YAML without applying it. Returns line/column errors and deprecation warnings."},
		func(ctx context.Context, req *mcp.CallToolRequest, in applyConfigIn) (*mcp.CallToolResult, map[string]any, error) {
			h.log(ctx, "validate_config", nil)
			b, err := bridgeOf(h)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ValidateConfig(ctx, in.YAML)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "plan_config", Description: "Diff YAML against the active revision before applying: which apps are added/removed/need-restart/apply-live, with affected process ids."},
		func(ctx context.Context, req *mcp.CallToolRequest, in applyConfigIn) (*mcp.CallToolResult, map[string]any, error) {
			h.log(ctx, "plan_config", nil)
			b, err := bridgeOf(h)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.PlanConfig(ctx, in.YAML)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "apply_config", Description: "Apply project YAML: validate, record a revision, hot-reload. Uses optimistic concurrency (base_revision from plan_config). Edit config only through apply_config."},
		func(ctx context.Context, req *mcp.CallToolRequest, in applyConfigIn) (*mcp.CallToolResult, map[string]any, error) {
			h.log(ctx, "apply_config", nil)
			b, err := bridgeOf(h)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ApplyConfig(ctx, in.YAML, in.BaseRevision, in.Message)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "resolve_config_proposal", Description: "Approve or dismiss a pending config proposal for auto-detected apps. Never approve on the user's behalf; always surface pending proposals first."},
		func(ctx context.Context, req *mcp.CallToolRequest, in resolveProposalIn) (*mcp.CallToolResult, map[string]any, error) {
			h.log(ctx, "resolve_config_proposal", in)
			b, err := bridgeOf(h)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ResolveProposal(ctx, in.ProposalID, in.Action)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "search_logs", Description: "Bounded full-text/regex search across a workspace's logs (not just one process tail). Returns matches with process ids."},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchLogsIn) (*mcp.CallToolResult, map[string]any, error) {
			h.log(ctx, "search_logs", in)
			b, err := bridgeOf(h)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.SearchLogs(ctx, in.Query, in.ProcessIDs, in.Regex, in.MaxRows)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "list_sessions", Description: "Who else is working here: connected agents and UIs with harness, workspace, and what they started. Processes are shared per project — re-attach instead of re-starting."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []map[string]any, error) {
			h.log(ctx, "list_sessions", nil)
			b, err := bridgeOf(h)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ListSessions(ctx)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})
}

// listScoped resolves the scope param for list_processes. The bridge
// scopes to its workspace by default; scope=all spans the machine.
func listScoped(h *handlers, scope string) (*api.ListResult, error) {
	if b, ok := h.rt.(*Bridge); ok {
		return b.ListScoped(scope)
	}
	return h.rt.List()
}

// ListScoped lists processes at workspace/project/all scope.
func (b *Bridge) ListScoped(scope string) (*api.ListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	all := scope == "all" || scope == "project"
	items, err := b.client.ProcessService().List(ctx, b.workspaceID, all)
	if err != nil {
		return nil, err
	}
	out := &api.ListResult{}
	for _, it := range items {
		var s api.ProcessSummary
		if err := convert(it, &s); err != nil {
			continue
		}
		out.Processes = append(out.Processes, s)
	}
	return out, nil
}
