package api

// EventDTO is a lifecycle event delivered to an explicit subscriber. It mirrors
// events.Event but is the stable wire form shared by the MCP server and CLI.
type EventDTO struct {
	// ID is the bus's monotonic per-bus sequence number; a subscriber uses it
	// as a `since` cursor to backfill without duplicates.
	ID         string `json:"id"`
	Type       string `json:"type"`
	ProcessID  string `json:"process_id"`
	InstanceID string `json:"instance_id,omitempty"`
	// Timestamp is the event time in RFC3339Nano.
	Timestamp string `json:"timestamp"`
	// Payload is the event's optional body (e.g. exit code, restart count).
	Payload any `json:"payload"`
}

// SubscribeEventsRequest is the input to subscribe_events.
type SubscribeEventsRequest struct {
	// ProcessID restricts delivery to a single process; empty subscribes to all.
	ProcessID string `json:"process_id,omitempty"`
	// Types restricts delivery to the listed event types (process.started,
	// process.exited, ...); empty subscribes to all.
	Types []string `json:"types,omitempty"`
	// Since is a backfill cursor: an event id to receive events after, "last"
	// to start fresh from now, or empty (same as "last").
	Since string `json:"since,omitempty"`
}

// SubscribeEventsResult is the response of subscribe_events.
type SubscribeEventsResult struct {
	SubscriptionID string `json:"subscription_id"`
}

// GetEventsRequest is the input to get_events.
type GetEventsRequest struct {
	SubscriptionID string `json:"subscription_id"`
	// Limit caps how many buffered events to drain; empty or <= 0 drains all.
	Limit int `json:"limit,omitempty"`
}

// GetEventsResult is the response of get_events.
type GetEventsResult struct {
	SubscriptionID string     `json:"subscription_id"`
	Events         []EventDTO `json:"events"`
	// Dropped counts events dropped since the previous get_events because the
	// subscription's ring buffer was full (the subscriber fell behind).
	Dropped int `json:"dropped"`
}

// EventStats is a snapshot of the subscription manager for a future event_stats
// tool: how many subscriptions are live and how many events have been dropped
// overall (per-subscription overruns and history-ring evictions).
type EventStats struct {
	Subscriptions int `json:"subscriptions"`
	Dropped       int `json:"dropped"`
}

// UnsubscribeEventsRequest is the input to unsubscribe_events.
type UnsubscribeEventsRequest struct {
	SubscriptionID string `json:"subscription_id"`
}

// UnsubscribeEventsResult is the response of unsubscribe_events.
type UnsubscribeEventsResult struct {
	SubscriptionID string `json:"subscription_id"`
	Unsubscribed   bool   `json:"unsubscribed"`
}

// AuditEntry is one audit-log line surfaced by get_audit_log.
type AuditEntry struct {
	Time       string         `json:"time"`
	Tool       string         `json:"tool"`
	Args       map[string]any `json:"args,omitempty"`
	Result     string         `json:"result"`
	DurationMS int64          `json:"duration_ms"`
	Caller     string         `json:"caller,omitempty"`
}

// GetAuditLogRequest is the input to get_audit_log.
type GetAuditLogRequest struct {
	Lines    int    `json:"lines,omitempty"` // default 100
	Contains string `json:"contains,omitempty"`
}

// AuditLogResult is the response of get_audit_log.
type AuditLogResult struct {
	Lines   int          `json:"lines"`
	Entries []AuditEntry `json:"entries"`
}
