package cli

import (
	"time"

	"agent-runtime/internal/daemon"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/pkg/client"
)

func ensureDaemonClient(tcpAddr string) (*client.Client, error) {
	p := paths.User()
	if err := p.EnsureDirs(); err != nil {
		return nil, err
	}
	if c, err := client.EnsureDaemon(p.SocketPath()); err == nil {
		return c, nil
	}
	d := daemon.New(p.SocketPath(), p.Data).WithTCP(tcpAddr)
	if err := d.Start(); err != nil {
		return nil, err
	}
	if err := d.WaitReady(5 * time.Second); err != nil {
		return nil, err
	}
	return client.EnsureDaemon(p.SocketPath())
}
