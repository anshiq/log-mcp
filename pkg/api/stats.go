package api

// ProcessStats is one row of the runtime_stats per-process summary: the
// lifecycle status and current in-memory log line counts for a managed
// process.
type ProcessStats struct {
	ProcessID   string `json:"process_id"`
	Status      string `json:"status"`
	Command     string `json:"command"`
	Profile     string `json:"profile,omitempty"`
	Restarts    int    `json:"restarts"`
	StdoutLines int    `json:"stdout_lines"`
	StderrLines int    `json:"stderr_lines"`
}

// RuntimeStats is the response of runtime_stats: a snapshot of the runtime's
// own health — how many processes exist in each lifecycle state, per-process
// log line counts, and the counters that reveal whether the log pipeline is
// keeping up (durable archive drops, forwarder drops, event subscription
// drops) plus uptime.
type RuntimeStats struct {
	TotalProcesses    int            `json:"total_processes"`
	ProcessesByStatus map[string]int `json:"processes_by_status"`
	Processes         []ProcessStats `json:"processes"`
	LogStoreDropped   uint64         `json:"log_store_dropped"`
	ForwardDropped    uint64         `json:"forward_dropped"`
	SubscriptionDrops uint64         `json:"subscription_drops"`
	UptimeSeconds     int64          `json:"uptime_seconds"`
}
