// Daemon-side client for the shim control socket (shim.sock).
// Used by reconnect-on-boot and by the ingest fast path: Hello for
// status/exit, Subscribe for live records from a cursor, Signal/Stdin/
// Stop/Release for control. Protocol versions [N-1, N] are supported;
// older shims degrade to orphan polling.
package shim

import (
	"fmt"
	"net"
	"time"

	"google.golang.org/protobuf/proto"

	shimv1 "agent-runtime/gen/agentruntime/shim/v1"
	"agent-runtime/internal/logpipe"
)

// Client talks to one shim over its control socket.
type Client struct {
	conn net.Conn
	sock string
}

// Dial opens the control socket.
func Dial(sock string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := net.DialTimeout("unix", sock, timeout)
	if err != nil {
		return nil, fmt.Errorf("shim: dial %s: %w", sock, err)
	}
	return &Client{conn: conn, sock: sock}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) roundtrip(op byte, req proto.Message, respOp byte, resp proto.Message) error {
	if err := c.send(op, req); err != nil {
		return err
	}
	// Skip heartbeats (op 0) while awaiting the response.
	for {
		gotOp, body, err := readFrame(c.conn)
		if err != nil {
			return err
		}
		if gotOp == 0 {
			continue
		}
		if gotOp == opError {
			var e shimv1.Error
			if err := proto.Unmarshal(body, &e); err != nil {
				return fmt.Errorf("shim %s: unreadable error frame", c.sock)
			}
			return fmt.Errorf("shim %s: %s", c.sock, e.Message)
		}
		if gotOp != respOp {
			return fmt.Errorf("shim %s: unexpected op %d (want %d)", c.sock, gotOp, respOp)
		}
		return proto.Unmarshal(body, resp)
	}
}

func (c *Client) send(op byte, msg proto.Message) error {
	return writeMsg(c.conn, op, msg)
}

// Hello negotiates the protocol and returns status. An unsupported shim
// version errors so the caller can degrade to orphan polling.
func (c *Client) Hello() (*shimv1.HelloResponse, error) {
	var resp shimv1.HelloResponse
	if err := c.roundtrip(opHello, &shimv1.HelloRequest{ProtocolVersion: int32(ProtocolVersion)}, opHelloResp, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Subscribe replays records from fromSeq, then streams live ones until
// ctx-like close: callers pass a stop channel; the returned channel
// closes on exit-record, error, or stop.
func (c *Client) Subscribe(fromSeq uint64, stop <-chan struct{}) (<-chan logpipe.Record, <-chan *shimv1.Exited, <-chan error) {
	recs := make(chan logpipe.Record, 256)
	exited := make(chan *shimv1.Exited, 1)
	errc := make(chan error, 1)
	go func() {
		defer close(recs)
		defer close(exited)
		defer close(errc)
		if err := c.send(opSubscribe, &shimv1.SubscribeRequest{FromSeq: int64(fromSeq)}); err != nil {
			errc <- err
			return
		}
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			op, body, err := readFrame(c.conn)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				errc <- err
				return
			}
			switch op {
			case 0: // heartbeat
				continue
			case opRecord:
				var fr shimv1.FramedRecord
				if err := proto.Unmarshal(body, &fr); err != nil {
					errc <- err
					return
				}
				select {
				case recs <- logpipe.Record{
					Seq: fr.Seq, TS: fr.TsUnixNs, Stream: uint8(fr.Stream),
					Flags: boolToPartial(fr.Partial), Payload: fr.Payload,
				}:
				case <-stop:
					return
				}
			case opExited:
				var ex shimv1.Exited
				if err := proto.Unmarshal(body, &ex); err != nil {
					errc <- err
					return
				}
				exited <- &ex
				return
			case opError:
				var e shimv1.Error
				_ = proto.Unmarshal(body, &e)
				errc <- fmt.Errorf("shim %s: %s", c.sock, e.Message)
				return
			default:
				errc <- fmt.Errorf("shim %s: unexpected op %d", c.sock, op)
				return
			}
		}
	}()
	return recs, exited, errc
}

func boolToPartial(p bool) uint8 {
	if p {
		return logpipe.FlagPartial
	}
	return 0
}

// Signal sends a named signal (group selects the process tree).
func (c *Client) Signal(signal string, group bool) error {
	var ack shimv1.HelloResponse
	return c.roundtrip(opSignal, &shimv1.SignalRequest{Signal: signal, Group: group}, opHelloResp, &ack)
}

// Stdin writes bytes to the child's stdin pipe.
func (c *Client) Stdin(data []byte) error {
	var ack shimv1.HelloResponse
	return c.roundtrip(opStdin, &shimv1.StdinRequest{Data: data}, opHelloResp, &ack)
}

// Stop asks the shim to SIGTERM the group, wait grace, then SIGKILL.
func (c *Client) Stop(grace time.Duration) error {
	var ack shimv1.HelloResponse
	return c.roundtrip(opStop, &shimv1.StopRequest{GraceMs: int32(grace.Milliseconds())}, opHelloResp, &ack)
}

// Release tells the shim the daemon recorded the exit; the shim exits.
func (c *Client) Release() error {
	var ack shimv1.HelloResponse
	return c.roundtrip(opRelease, &shimv1.ReleaseRequest{}, opHelloResp, &ack)
}
