// Typed clients for the remaining v3 services. Transport and calling
// convention live in client.go; this file adds Process/Log/Event/
// Session/Config/Audit/Integration/Project methods mirroring the
// server routes.
//
// Payloads use pkg/api structs (snake_case) wherever a handler decodes
// them, keeping MCP bridge ↔ daemon shapes identical.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"agent-runtime/pkg/api"
)

func bytesReader(b []byte) io.Reader {
	if len(b) == 0 {
		return nil
	}
	return bytes.NewReader(b)
}

// streamLines opens an NDJSON stream and decodes messages until ctx ends.
func (c *Client) streamLines(ctx context.Context, service, method string, req any) (<-chan map[string]any, error) {
	var body []byte
	if req != nil {
		var err error
		if body, err = json.Marshal(req); err != nil {
			return nil, err
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		"http://agentd/agentruntime.v1."+service+"/"+method, bytesReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(ClientHeader, c.agent)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("agentd %s/%s: %s", service, method, resp.Status)
	}
	ch := make(chan map[string]any, 1024)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			var msg map[string]any
			if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
				continue
			}
			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// --- Process ---

func (c *ProcessServiceClient) Stop(ctx context.Context, processID string) error {
	return c.client.call(ctx, "ProcessService", "Stop", map[string]any{"processId": processID}, nil)
}

func (c *ProcessServiceClient) Restart(ctx context.Context, processID string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ProcessService", "Restart", map[string]any{"processId": processID}, &out)
	return out, err
}

func (c *ProcessServiceClient) Get(ctx context.Context, processID string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ProcessService", "Get", map[string]any{"processId": processID}, &out)
	return out, err
}

func (c *ProcessServiceClient) List(ctx context.Context, workspaceID string, all bool) ([]map[string]any, error) {
	var out struct {
		Processes []map[string]any `json:"processes"`
	}
	err := c.client.call(ctx, "ProcessService", "List",
		map[string]any{"workspaceId": workspaceID, "allWorkspaces": all}, &out)
	return out.Processes, err
}

func (c *ProcessServiceClient) Signal(ctx context.Context, processID, signal string) error {
	return c.client.call(ctx, "ProcessService", "Signal",
		api.SignalProcessRequest{ProcessID: processID, Signal: signal}, nil)
}

func (c *ProcessServiceClient) SendStdin(ctx context.Context, processID, data string) error {
	return c.client.call(ctx, "ProcessService", "SendStdin",
		map[string]any{"processId": processID, "data": data}, nil)
}

func (c *ProcessServiceClient) Remove(ctx context.Context, processID string, force bool) error {
	return c.client.call(ctx, "ProcessService", "Remove",
		map[string]any{"processId": processID, "force": force}, nil)
}

func (c *ProcessServiceClient) WaitForExit(ctx context.Context, processID string, timeoutMs int) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ProcessService", "WaitForExit",
		api.WaitForExitParams{ProcessID: processID, TimeoutMS: timeoutMs}, &out)
	return out, err
}

func (c *ProcessServiceClient) SetRestartPolicy(ctx context.Context, processID, policy string) error {
	return c.client.call(ctx, "ProcessService", "SetRestartPolicy",
		api.SetRestartPolicyRequest{ProcessID: processID, Policy: policy}, nil)
}

func (c *ProcessServiceClient) GetEnv(ctx context.Context, processID string, reveal, live bool) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ProcessService", "GetEnv",
		api.ProcessEnvRequest{ProcessID: processID, Reveal: reveal, Live: live}, &out)
	return out, err
}

func (c *ProcessServiceClient) Watch(ctx context.Context, workspaceID string, all bool) (<-chan map[string]any, error) {
	return c.client.streamLines(ctx, "ProcessService", "WatchProcesses",
		map[string]any{"workspaceId": workspaceID, "allWorkspaces": all})
}

// --- Log ---

func (c *LogServiceClient) GetLogs(ctx context.Context, processID string, lines int) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "LogService", "GetLogs",
		api.GetLogsRequest{ProcessID: processID, Lines: lines}, &out)
	return out, err
}

func (c *LogServiceClient) Search(ctx context.Context, req map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "LogService", "SearchLogs", req, &out)
	return out, err
}

func (c *LogServiceClient) Tail(ctx context.Context, req map[string]any) (<-chan map[string]any, error) {
	return c.client.streamLines(ctx, "LogService", "TailLogs", req)
}

func (c *LogServiceClient) WaitForLog(ctx context.Context, req map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "LogService", "WaitForLog", req, &out)
	return out, err
}

func (c *LogServiceClient) Clear(ctx context.Context, processID, stream string) error {
	return c.client.call(ctx, "LogService", "ClearLogs",
		map[string]any{"processId": processID, "stream": stream}, nil)
}

// --- Event ---

func (c *EventServiceClient) List(ctx context.Context, req map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "EventService", "ListEvents", req, &out)
	return out, err
}

func (c *EventServiceClient) Watch(ctx context.Context, req map[string]any) (<-chan map[string]any, error) {
	return c.client.streamLines(ctx, "EventService", "WatchEvents", req)
}

// --- Session ---

func (c *SessionServiceClient) List(ctx context.Context, workspaceID string) ([]map[string]any, error) {
	var out struct {
		Sessions []map[string]any `json:"sessions"`
	}
	err := c.client.call(ctx, "SessionService", "ListSessions",
		map[string]any{"workspaceId": workspaceID}, &out)
	return out.Sessions, err
}

func (c *SessionServiceClient) Close(ctx context.Context, sessionID string) error {
	return c.client.call(ctx, "SessionService", "Close", map[string]any{"sessionId": sessionID}, nil)
}

// Ping is the unary heartbeat for long-lived bridges.
func (c *SessionServiceClient) Ping(ctx context.Context, sessionID string) error {
	return c.client.call(ctx, "SessionService", "Ping", map[string]any{"sessionId": sessionID}, nil)
}

// --- Config ---

func (c *ConfigServiceClient) Get(ctx context.Context, workspaceID string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ConfigService", "GetConfig", map[string]any{"workspaceId": workspaceID}, &out)
	return out, err
}

func (c *ConfigServiceClient) Validate(ctx context.Context, yaml string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ConfigService", "Validate", map[string]any{"yaml": yaml}, &out)
	return out, err
}

func (c *ConfigServiceClient) PlanYAML(ctx context.Context, workspaceID, yaml string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ConfigService", "Plan",
		map[string]any{"workspaceId": workspaceID, "yaml": yaml}, &out)
	return out, err
}

func (c *ConfigServiceClient) Apply(ctx context.Context, req map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ConfigService", "Apply", req, &out)
	return out, err
}

func (c *ConfigServiceClient) Revisions(ctx context.Context, projectID string) ([]map[string]any, error) {
	var out struct {
		Revisions []map[string]any `json:"revisions"`
	}
	err := c.client.call(ctx, "ConfigService", "ListRevisions",
		map[string]any{"projectId": projectID}, &out)
	return out.Revisions, err
}

func (c *ConfigServiceClient) Rollback(ctx context.Context, projectID string, revision int64) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ConfigService", "Rollback",
		map[string]any{"projectId": projectID, "revision": revision}, &out)
	return out, err
}

// --- Audit ---

type AuditServiceClient struct {
	client *Client
}

func (c *Client) AuditService() *AuditServiceClient { return &AuditServiceClient{c} }

func (c *AuditServiceClient) List(ctx context.Context, workspaceID string, limit int) ([]map[string]any, error) {
	var out struct {
		Entries []map[string]any `json:"entries"`
	}
	err := c.client.call(ctx, "AuditService", "ListAudit",
		map[string]any{"workspaceId": workspaceID, "limit": limit}, &out)
	return out.Entries, err
}

// --- Integration ---

type IntegrationServiceClient struct {
	client *Client
}

func (c *Client) IntegrationService() *IntegrationServiceClient {
	return &IntegrationServiceClient{c}
}

func (c *IntegrationServiceClient) Harnesses(ctx context.Context) ([]map[string]any, error) {
	var out struct {
		Harnesses []map[string]any `json:"harnesses"`
	}
	err := c.client.call(ctx, "IntegrationService", "ListHarnesses", map[string]any{}, &out)
	return out.Harnesses, err
}

func (c *IntegrationServiceClient) Preview(ctx context.Context, harness, scope, kind string) (string, error) {
	var out struct {
		Diff string `json:"diff"`
	}
	err := c.client.call(ctx, "IntegrationService", "PreviewInstall",
		map[string]any{"harness": harness, "scope": scope, "kind": kind}, &out)
	return out.Diff, err
}

// --- Project extras ---

func (c *ProjectServiceClient) GetProject(ctx context.Context, projectID string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ProjectService", "GetProject", map[string]any{"projectId": projectID}, &out)
	return out, err
}

func (c *ProjectServiceClient) TrustRepo(ctx context.Context, workspaceID, path, sha string) error {
	return c.client.call(ctx, "ProjectService", "TrustRepoConfig",
		map[string]any{"workspaceId": workspaceID, "path": path, "sha256": sha}, nil)
}

func (c *ProjectServiceClient) Forget(ctx context.Context, projectID string) error {
	return c.client.call(ctx, "ProjectService", "ForgetProject", map[string]any{"projectId": projectID}, nil)
}
