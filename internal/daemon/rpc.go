package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"time"

	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// rpcRequest/rpcResponse are the wire envelope for the local Unix-socket RPC.
// They reuse pkg/api types verbatim as the method params/results, so the
// transport carries exactly the same data the local facade returns.
type rpcRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// handler invokes one facade method. params is the raw JSON method args; it
// returns the method's result value (marshaled by the caller).
type handler func(params json.RawMessage) (any, error)

// Server exposes a runtime.Facade over a Unix socket. One request per
// connection keeps framing trivial and per-call isolation airtight (a hung
// client can only stall its own connection).
type Server struct {
	facade   runtime.Facade
	ln       net.Listener
	handlers map[string]handler
	done     chan struct{}
}

// NewServer listens on sockPath (removing any stale socket first) and serves
// the facade. Call Close to stop it.
func NewServer(f runtime.Facade, sockPath string) (*Server, error) {
	_ = removeSocket(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	s := &Server{
		facade:   f,
		ln:       ln,
		handlers: buildHandlers(f),
		done:     make(chan struct{}),
	}
	go s.acceptLoop()
	return s, nil
}

func removeSocket(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				return
			}
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	var req rpcRequest
	if err := dec.Decode(&req); err != nil {
		return
	}
	resp := rpcResponse{ID: req.ID}
	h, ok := s.handlers[req.Method]
	if !ok {
		resp.Error = "unknown method " + req.Method
	} else {
		result, err := h(req.Params)
		if err != nil {
			resp.Error = err.Error()
		} else {
			b, _ := json.Marshal(result)
			resp.Result = b
		}
	}
	_ = json.NewEncoder(conn).Encode(&resp)
}

// Close stops accepting and closes the listener.
func (s *Server) Close() {
	select {
	case <-s.done:
		return
	default:
		close(s.done)
	}
	s.ln.Close()
}

// buildHandlers maps facade methods to their dispatch closures.
func buildHandlers(f runtime.Facade) map[string]handler {
	ctx := context.Background()
	return map[string]handler{
		"Start": func(p json.RawMessage) (any, error) {
			var req api.StartRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.Start(ctx, req)
		},
		"Stop": func(p json.RawMessage) (any, error) {
			var req struct {
				ProcessID string `json:"process_id"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return nil, f.Stop(ctx, req.ProcessID)
		},
		"Restart": func(p json.RawMessage) (any, error) {
			var req struct {
				ProcessID string `json:"process_id"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return nil, f.Restart(ctx, req.ProcessID)
		},
		"SetRestartPolicy": func(p json.RawMessage) (any, error) {
			var req api.SetRestartPolicyRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.SetRestartPolicy(req)
		},
		"Status": func(p json.RawMessage) (any, error) {
			var req struct {
				ProcessID string `json:"process_id"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.Status(req.ProcessID)
		},
		"List": func(json.RawMessage) (any, error) { return f.List() },
		"GetLogs": func(p json.RawMessage) (any, error) {
			var req api.GetLogsRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.GetLogs(req)
		},
		"ClearLogs": func(p json.RawMessage) (any, error) {
			var req struct {
				ProcessID string `json:"process_id"`
				Stream    string `json:"stream"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.ClearLogs(req.ProcessID, req.Stream)
		},
		"SendStdin": func(p json.RawMessage) (any, error) {
			var req struct {
				ProcessID string `json:"process_id"`
				Data      string `json:"data"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.SendStdin(req.ProcessID, req.Data)
		},
		"RemoveProcess": func(p json.RawMessage) (any, error) {
			var req struct {
				ProcessID string `json:"process_id"`
				Force     bool   `json:"force"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.RemoveProcess(req.ProcessID, req.Force)
		},
		"WaitForLog": func(p json.RawMessage) (any, error) {
			var req api.WaitForLogRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.WaitForLog(ctx, req)
		},
		"WaitForExit": func(p json.RawMessage) (any, error) {
			var req api.WaitForExitParams
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.WaitForExit(ctx, req)
		},
		"Apps": func(json.RawMessage) (any, error) { return f.Apps() },
		"SignalProcess": func(p json.RawMessage) (any, error) {
			var req api.SignalProcessRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.SignalProcess(ctx, req)
		},
		"ProcessEnv": func(p json.RawMessage) (any, error) {
			var req api.ProcessEnvRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.ProcessEnv(req)
		},
		"OpenShell": func(p json.RawMessage) (any, error) {
			var req api.OpenShellRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.OpenShell(ctx, req)
		},
		"SubscribeEvents": func(p json.RawMessage) (any, error) {
			var req struct {
				Client string                     `json:"client"`
				Req    api.SubscribeEventsRequest `json:"req"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.SubscribeEvents(req.Client, req.Req)
		},
		"GetEvents": func(p json.RawMessage) (any, error) {
			var req api.GetEventsRequest
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.GetEvents(req)
		},
		"UnsubscribeEvents": func(p json.RawMessage) (any, error) {
			var req struct {
				Client         string `json:"client"`
				SubscriptionID string `json:"subscription_id"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.UnsubscribeEvents(req.Client, req.SubscriptionID)
		},
		"RuntimeStats": func(json.RawMessage) (any, error) { return f.RuntimeStats() },
		"GetAuditLog": func(p json.RawMessage) (any, error) {
			var req struct {
				Lines    int    `json:"lines"`
				Contains string `json:"contains"`
			}
			if err := json.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return f.GetAuditLog(req.Lines, req.Contains)
		},
	}
}

// Client is a runtime.Facade that forwards pkg/api requests to a daemon over a
// Unix socket. It dials per call (local, low call rate) so each RPC is
// isolated and a transient daemon restart never poisons later calls.
type Client struct {
	sockPath string
	nextID   atomic.Uint64
}

// NewClient returns a client that talks to the daemon listening on sockPath.
func NewClient(sockPath string) *Client { return &Client{sockPath: sockPath} }

func (c *Client) call(method string, params, out any) error {
	return c.callTimeout(method, params, out, 60*time.Second)
}

func (c *Client) callTimeout(method string, params, out any, timeout time.Duration) error {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return errors.New("daemon is not running; start it with `agent-runtime daemon start`: " + err.Error())
	}
	defer conn.Close()
	req := rpcRequest{ID: c.nextID.Add(1), Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = b
	}
	if timeout > 0 {
		conn.SetDeadline(time.Now().Add(timeout))
	}
	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		return err
	}
	var resp rpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

var _ runtime.Facade = (*Client)(nil)

// --- Facade methods (thin forwarding) ---

func (c *Client) Start(_ context.Context, req api.StartRequest) (*api.StartResult, error) {
	var out api.StartResult
	if err := c.call("Start", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Stop(_ context.Context, id string) error {
	return c.call("Stop", struct {
		ProcessID string `json:"process_id"`
	}{id}, nil)
}

func (c *Client) Restart(_ context.Context, id string) error {
	return c.call("Restart", struct {
		ProcessID string `json:"process_id"`
	}{id}, nil)
}

func (c *Client) SetRestartPolicy(req api.SetRestartPolicyRequest) (*api.SetRestartPolicyResult, error) {
	var out api.SetRestartPolicyResult
	if err := c.call("SetRestartPolicy", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Status(id string) (*api.StatusResult, error) {
	var out api.StatusResult
	if err := c.call("Status", struct {
		ProcessID string `json:"process_id"`
	}{id}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) List() (*api.ListResult, error) {
	var out api.ListResult
	if err := c.call("List", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetLogs(req api.GetLogsRequest) (*api.GetLogsResult, error) {
	var out api.GetLogsResult
	if err := c.call("GetLogs", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ClearLogs(id, stream string) (*api.ClearLogsResult, error) {
	var out api.ClearLogsResult
	if err := c.call("ClearLogs", struct {
		ProcessID string `json:"process_id"`
		Stream    string `json:"stream"`
	}{id, stream}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SendStdin(id, data string) (*api.SendStdinResult, error) {
	var out api.SendStdinResult
	if err := c.call("SendStdin", struct {
		ProcessID string `json:"process_id"`
		Data      string `json:"data"`
	}{id, data}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RemoveProcess(id string, force bool) (*api.RemoveProcessResult, error) {
	var out api.RemoveProcessResult
	if err := c.call("RemoveProcess", struct {
		ProcessID string `json:"process_id"`
		Force     bool   `json:"force"`
	}{id, force}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) WaitForLog(_ context.Context, req api.WaitForLogRequest) (*api.WaitForLogResult, error) {
	var out api.WaitForLogResult
	timeout := 60 * time.Second
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS)*time.Millisecond + 5*time.Second
	}
	if err := c.callTimeout("WaitForLog", req, &out, timeout); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) WaitForExit(_ context.Context, req api.WaitForExitParams) (*api.WaitForExitResult, error) {
	var out api.WaitForExitResult
	timeout := 60 * time.Second
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS)*time.Millisecond + 5*time.Second
	}
	if err := c.callTimeout("WaitForExit", req, &out, timeout); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Apps() (*api.ListAppsResult, error) {
	var out api.ListAppsResult
	if err := c.call("Apps", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SignalProcess(_ context.Context, req api.SignalProcessRequest) (*api.SignalProcessResult, error) {
	var out api.SignalProcessResult
	if err := c.call("SignalProcess", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ProcessEnv(req api.ProcessEnvRequest) (*api.ProcessEnvResult, error) {
	var out api.ProcessEnvResult
	if err := c.call("ProcessEnv", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OpenShell(_ context.Context, req api.OpenShellRequest) (*api.StartResult, error) {
	var out api.StartResult
	if err := c.call("OpenShell", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SubscribeEvents(client string, req api.SubscribeEventsRequest) (*api.SubscribeEventsResult, error) {
	var out api.SubscribeEventsResult
	if err := c.call("SubscribeEvents", struct {
		Client string                     `json:"client"`
		Req    api.SubscribeEventsRequest `json:"req"`
	}{client, req}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEvents(req api.GetEventsRequest) (*api.GetEventsResult, error) {
	var out api.GetEventsResult
	if err := c.call("GetEvents", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UnsubscribeEvents(client, subscriptionID string) (*api.UnsubscribeEventsResult, error) {
	var out api.UnsubscribeEventsResult
	if err := c.call("UnsubscribeEvents", struct {
		Client         string `json:"client"`
		SubscriptionID string `json:"subscription_id"`
	}{client, subscriptionID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RuntimeStats() (*api.RuntimeStats, error) {
	var out api.RuntimeStats
	if err := c.call("RuntimeStats", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetAuditLog(lines int, contains string) (*api.AuditLogResult, error) {
	var out api.AuditLogResult
	if err := c.call("GetAuditLog", struct {
		Lines    int    `json:"lines"`
		Contains string `json:"contains"`
	}{lines, contains}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
