package server

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cpuSample struct {
	cpu int64
	at  time.Time
}

var cpuSamples = map[string]cpuSample{}
var cpuMu sync.Mutex

func usagePayload(processID string, pid int, mem, cpu int64, ports []int) map[string]any {
	cpuMu.Lock()
	prev, ok := cpuSamples[processID]
	now := time.Now()
	cpuSamples[processID] = cpuSample{cpu: cpu, at: now}
	cpuMu.Unlock()
	percent := 0.0
	if ok && cpu >= prev.cpu {
		dt := now.Sub(prev.at).Seconds()
		if dt > 0 {
			percent = float64(cpu-prev.cpu) / 1e9 / dt * 100
		}
	}
	rss, threads, fds, children := procDetails(pid)
	return map[string]any{
		"processId": processID, "pid": pid,
		"memoryBytes": mem, "cpuNanos": cpu, "cpuPercent": percent,
		"rssBytes": rss, "threads": threads, "openFds": fds,
		"children": children, "ports": ports,
		"at": now.UnixNano(),
	}
}

func procDetails(pid int) (int64, int, int, int) {
	if pid <= 0 {
		return 0, 0, 0, 0
	}
	base := "/proc/" + strconv.Itoa(pid)
	data, err := os.ReadFile(base + "/status")
	if err != nil {
		return 0, 0, 0, 0
	}
	var rss int64
	threads := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				if v, err := strconv.ParseInt(f[1], 10, 64); err == nil {
					rss = v * 1024
				}
			}
		} else if strings.HasPrefix(line, "Threads:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				if v, err := strconv.Atoi(f[1]); err == nil {
					threads = v
				}
			}
		}
	}
	fds := 0
	if entries, err := os.ReadDir(base + "/fd"); err == nil {
		fds = len(entries)
	}
	children := 0
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, e := range entries {
			if _, err := strconv.Atoi(e.Name()); err != nil {
				continue
			}
			if e.Name() == strconv.Itoa(pid) {
				continue
			}
			if d, err := os.ReadFile("/proc/" + e.Name() + "/stat"); err == nil {
				if idx := strings.LastIndex(string(d), ")"); idx > 0 {
					rest := strings.Fields(string(d)[idx+1:])
					if len(rest) >= 2 && rest[1] == strconv.Itoa(pid) {
						children++
					}
				}
			}
		}
	}
	return rss, threads, fds, children
}
