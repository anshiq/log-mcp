//go:build linux
// +build linux

package process

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TreePIDs returns the root pid plus all descendant pids (subreaper/pgid
// tracking covers abrupt exits; /proc is the ground truth here).
func TreePIDs(root int) []int {
	ppid := map[int]int{}
	pids := []int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return []int{root}
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		pids = append(pids, pid)
		if pp, err := procPPID(pid); err == nil {
			ppid[pid] = pp
		}
	}
	inTree := map[int]bool{root: true}
	changed := true
	for changed {
		changed = false
		for pid, pp := range ppid {
			if !inTree[pid] && inTree[pp] {
				inTree[pid] = true
				changed = true
			}
		}
	}
	var out []int
	for pid := range inTree {
		out = append(out, pid)
	}
	return out
}

func procPPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, fmt.Errorf("bad stat")
	}
	fields := strings.Fields(s[i+2:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("bad stat")
	}
	return strconv.Atoi(fields[1])
}

// ListeningPortsForPID returns sorted TCP ports in LISTEN state owned by
// the process tree rooted at pid, by joining /proc/net/tcp* (inode) with
// /proc/<pid>/fd socket links. Best-effort: empty on any error.
func ListeningPortsForPID(pid int) []int {
	if pid <= 0 {
		return nil
	}
	tree := TreePIDs(pid)
	inTree := map[int]bool{}
	for _, p := range tree {
		inTree[p] = true
	}
	listen := map[int]int{} // inode -> port for LISTEN sockets
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		for inode, port := range listenPorts(table) {
			listen[inode] = port
		}
	}
	ports := map[int]bool{}
	for pid := range inTree {
		fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fmt.Sprintf("/proc/%d/fd", pid), fd.Name()))
			if err != nil {
				continue
			}
			var inode int
			if _, err := fmt.Sscanf(target, "socket:[%d]", &inode); err != nil {
				continue
			}
			if port, ok := listen[inode]; ok {
				ports[port] = true
			}
		}
	}
	var out []int
	for p := range ports {
		out = append(out, p)
	}
	sortInts(out)
	return out
}

// listenPorts returns inode→port for LISTEN (0A) sockets in a table.
func listenPorts(table string) map[int]int {
	out := map[int]int{}
	data, err := os.ReadFile(table)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		if f[3] != "0A" { // TCP_LISTEN
			continue
		}
		inode, err := strconv.Atoi(f[9])
		if err != nil {
			continue
		}
		parts := strings.Split(f[1], ":")
		if len(parts) != 2 {
			continue
		}
		if port, err := strconv.ParseUint(parts[1], 16, 32); err == nil {
			out[inode] = int(port)
		}
	}
	return out
}

func sortInts(v []int) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
