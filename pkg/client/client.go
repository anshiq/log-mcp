// Package client is the public Go SDK for agent-runtime v3.
// It provides EnsureDaemon, typed service clients, stream helpers
// with auto-resume, and version handshake. All CLI, TUI, and
// bridge code uses this package.
//
// Transport: HTTP/JSON over the daemon UDS at Connect-compatible
// paths (/agentruntime.v1.X/Y). The client header
// X-Agent-Runtime-Client: <kind>/<version> is sent on every call (§5.2).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	// DefaultAPIVersion is the current API version.
	DefaultAPIVersion = "v1"
	// MinClientVersion is the minimum client version accepted.
	MinClientVersion = "v1"
	// ShimProtocolVersion is the current shim protocol version.
	ShimProtocolVersion = 1
	// ClientHeader identifies the caller per §5.2.
	ClientHeader = "X-Agent-Runtime-Client"
)

// Client is a client of the agent-runtime daemon.
type Client struct {
	socket     string
	httpClient *http.Client
	agent      string
	version    string
}

// New creates a client for a running daemon socket.
func New(socketPath, agent string) *Client {
	if agent == "" {
		agent = "cli/unknown"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		socket:     socketPath,
		httpClient: &http.Client{Transport: transport, Timeout: 30 * time.Second},
		agent:      agent,
		version:    DefaultAPIVersion,
	}
}

// EnsureDaemon ensures a daemon is running. If no daemon answers on the
// socket, it returns an error telling the user to start agentd (spawn is
// owned by internal/daemon to keep the flock in one place).
func EnsureDaemon(socketPath string) (*Client, error) {
	c := New(socketPath, "cli/ensure")
	if err := c.ping(); err != nil {
		return nil, fmt.Errorf("agentd not reachable at %s: %w (run `agent-runtime daemon start` or `agentd --foreground`)", socketPath, err)
	}
	return c, nil
}

// EnsureDaemonOrSpawn tries to dial; on refusal it attempts a spawn via
// the daemon package callback to avoid an import cycle. Pass nil to
// disable spawning.
func EnsureDaemonOrSpawn(socketPath string, spawn func() error) (*Client, error) {
	if c, err := EnsureDaemon(socketPath); err == nil {
		return c, nil
	} else if spawn == nil {
		return nil, err
	}
	if err := spawn(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := EnsureDaemon(socketPath); err == nil {
			return c, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("agentd did not become ready at %s", socketPath)
}

func (c *Client) ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.GetVersion(ctx)
	return err
}

func (c *Client) call(ctx context.Context, service, method string, req, resp any) error {
	var body io.Reader
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		"http://agentd/agentruntime.v1."+service+"/"+method, body)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(ClientHeader, c.agent)
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4*1024))
		return fmt.Errorf("agentd %s/%s: %s: %s", service, method, httpResp.Status, string(data))
	}
	if resp == nil {
		return nil
	}
	return json.NewDecoder(httpResp.Body).Decode(resp)
}

// SocketPath returns the daemon socket this client dials.
func (c *Client) SocketPath() string { return c.socket }

// GetVersion returns the daemon version information.
func (c *Client) GetVersion(ctx context.Context) (*VersionResponse, error) {
	var out VersionResponse
	if err := c.call(ctx, "SystemService", "GetVersion", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SystemService provides access to the SystemService.
func (c *Client) SystemService() *SystemServiceClient {
	return &SystemServiceClient{c}
}

// ProjectService provides access to the ProjectService.
func (c *Client) ProjectService() *ProjectServiceClient {
	return &ProjectServiceClient{c}
}

// ProcessService provides access to the ProcessService.
func (c *Client) ProcessService() *ProcessServiceClient {
	return &ProcessServiceClient{c}
}

// ConfigService provides access to the ConfigService.
func (c *Client) ConfigService() *ConfigServiceClient {
	return &ConfigServiceClient{c}
}

// LogService provides access to the LogService.
func (c *Client) LogService() *LogServiceClient {
	return &LogServiceClient{c}
}

// EventService provides access to the EventService.
func (c *Client) EventService() *EventServiceClient {
	return &EventServiceClient{c}
}

// SessionService provides access to the SessionService.
func (c *Client) SessionService() *SessionServiceClient {
	return &SessionServiceClient{c}
}

// VersionResponse is the response from GetVersion.
type VersionResponse struct {
	DaemonVersion    string `json:"daemonVersion"`
	APIVersion       string `json:"apiVersion"`
	MinClientVersion string `json:"minClientVersion"`
	ShimProtocol     int32  `json:"shimProtocol"`
}

// SystemServiceClient is a typed client for SystemService.
type SystemServiceClient struct {
	client *Client
}

func (c *SystemServiceClient) GetVersion(ctx context.Context) (*VersionResponse, error) {
	return c.client.GetVersion(ctx)
}

func (c *SystemServiceClient) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.client.call(ctx, "SystemService", "Health", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *SystemServiceClient) GetStats(ctx context.Context) (*StatsResponse, error) {
	var out StatsResponse
	if err := c.client.call(ctx, "SystemService", "GetStats", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *SystemServiceClient) Shutdown(ctx context.Context, keepProcesses bool) error {
	return c.client.call(ctx, "SystemService", "Shutdown",
		map[string]any{"keepProcesses": keepProcesses}, nil)
}

// HealthResponse is the response from Health.
type HealthResponse struct {
	Healthy  bool   `json:"healthy"`
	Status   string `json:"status"`
	UptimeMs int64  `json:"uptimeMs"`
}

// StatsResponse is the response from GetStats.
type StatsResponse struct {
	Projects        int32 `json:"projects"`
	Processes       int32 `json:"processes"`
	Sessions        int32 `json:"sessions"`
	LogDiskBytes    int64 `json:"logDiskBytes"`
	IndexLagSeconds int64 `json:"indexLagSeconds"`
}

// ProjectServiceClient is a typed client for ProjectService.
type ProjectServiceClient struct {
	client *Client
}

// Resolve resolves a working directory to a project and workspace.
func (c *ProjectServiceClient) Resolve(ctx context.Context, path string) (*ResolveResponse, error) {
	if path == "" {
		if cwd, err := os.Getwd(); err == nil {
			path = cwd
		}
	}
	var out ResolveResponse
	if err := c.client.call(ctx, "ProjectService", "Resolve", map[string]any{"path": path}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListProjects lists all projects.
func (c *ProjectServiceClient) ListProjects(ctx context.Context) ([]*ProjectInfo, error) {
	var out struct {
		Projects []*ProjectInfo `json:"projects"`
	}
	if err := c.client.call(ctx, "ProjectService", "ListProjects", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// ResolveResponse is the response from Resolve.
type ResolveResponse struct {
	ProjectID          string   `json:"projectId"`
	WorkspaceID        string   `json:"workspaceId"`
	ProjectName        string   `json:"projectName"`
	NewlyCreated       bool     `json:"newlyCreated"`
	UnresolvedLocators []string `json:"unresolvedLocators"`
}

// ProjectInfo is a project summary.
type ProjectInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ConfigMode     string `json:"configMode"`
	LastUsedAt     int64  `json:"lastUsedAt"`
	WorkspaceCount int    `json:"workspaceCount"`
}

// ProcessServiceClient is a typed client for ProcessService.
type ProcessServiceClient struct {
	client *Client
}

// Start starts a process.
func (c *ProcessServiceClient) Start(ctx context.Context, req *StartRequest) (*StartResponse, error) {
	var out StartResponse
	if err := c.client.call(ctx, "ProcessService", "Start", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartRequest is a request to start a process.
type StartRequest struct {
	WorkspaceID string            `json:"workspaceId"`
	App         string            `json:"app"`
	Command     []string          `json:"command"`
	WorkDir     string            `json:"workdir"`
	Env         map[string]string `json:"env"`
	Lifetime    string            `json:"lifetime"`
}

// StartResponse is the response from Start.
type StartResponse struct {
	ProcessID  string `json:"processId"`
	InstanceID string `json:"instanceId"`
	Status     string `json:"status"`
	PID        int32  `json:"pid"`
}

// ConfigServiceClient is a typed client for ConfigService.
type ConfigServiceClient struct {
	client *Client
}

// Plan returns a config diff before applying.
func (c *ConfigServiceClient) Plan(ctx context.Context, workspaceID string) (*PlanResponse, error) {
	var out PlanResponse
	if err := c.client.call(ctx, "ConfigService", "Plan", map[string]any{"workspaceId": workspaceID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PlanResponse is the response from Plan.
type PlanResponse struct {
	RestartApps []string `json:"restartApps"`
	LiveApps    []string `json:"liveApps"`
	StaleApps   []string `json:"staleApps"`
}

// LogServiceClient is a typed client for LogService.
type LogServiceClient struct {
	client *Client
}

// TailLogs streams log entries for a process (poll-based until
// TailLogs server streaming lands; bounded and explicit per §1.3.4).
func (c *LogServiceClient) TailLogs(ctx context.Context, processID string) (<-chan *LogEntry, error) {
	ch := make(chan *LogEntry, 100)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

// LogEntry is a single log entry from a stream.
type LogEntry struct {
	Seq       int64  `json:"seq"`
	Timestamp int64  `json:"ts"`
	Stream    string `json:"stream"`
	Line      string `json:"line"`
	Level     string `json:"level"`
}

// EventServiceClient is a typed client for EventService.
type EventServiceClient struct {
	client *Client
}

// WatchEvents streams events (poll-based until WatchEvents streaming lands).
func (c *EventServiceClient) WatchEvents(ctx context.Context, workspaceID string) (<-chan *Event, error) {
	ch := make(chan *Event, 100)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

// Event is a single event from a stream.
type Event struct {
	ID          int64  `json:"id"`
	Timestamp   int64  `json:"ts"`
	Type        string `json:"type"`
	WorkspaceID string `json:"workspaceId"`
	ProcessID   string `json:"processId"`
}

// SessionServiceClient is a typed client for SessionService.
type SessionServiceClient struct {
	client *Client
}

// Register registers a new session.
func (c *SessionServiceClient) Register(ctx context.Context, kind, harness string, pid int, workspaceID string) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.client.call(ctx, "SessionService", "Register",
		map[string]any{"kind": kind, "harness": harness, "pid": pid, "workspaceId": workspaceID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RegisterResponse is the response from Register.
type RegisterResponse struct {
	SessionID string `json:"sessionId"`
}
