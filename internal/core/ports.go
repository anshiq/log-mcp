// Port templating (${port:name}, §13): two workspaces of one project
// would otherwise collide on :3000. The daemon allocates a stable port
// per (workspace, name): the default for the first workspace, else the
// next free port. Allocations persist in settings and show in the GUI.
//
// Expansion happens once at config load (ProjectRuntime), so every start
// path (Start, StartStack, bridge, CLI) sees concrete values.
package core

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// AllocatePort returns the stable port for (workspace, name).
func (e *Engine) AllocatePort(workspaceID, name string, def int) (int, error) {
	key := "ports/" + workspaceID + "/" + name
	if v, ok, _ := e.store.GetSetting(key); ok {
		if p, err := strconv.Atoi(strings.Trim(v, `"`)); err == nil && p > 0 {
			return p, nil
		}
	}
	// First workspace tries the declared default when free and unclaimed.
	if def > 0 && !portClaimed(e, def, workspaceID) && portFree(def) {
		_ = e.store.SetSetting(key, strconv.Itoa(def))
		_ = e.store.SetSetting(fmt.Sprintf("port-owner/%d", def), `"`+workspaceID+`"`)
		return def, nil
	}
	p, err := freePort()
	if err != nil {
		return 0, err
	}
	_ = e.store.SetSetting(key, strconv.Itoa(p))
	return p, nil
}

func portClaimed(e *Engine, port int, workspaceID string) bool {
	v, ok, _ := e.store.GetSetting(fmt.Sprintf("port-owner/%d", port))
	return ok && strings.Trim(v, `"`) != workspaceID
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
