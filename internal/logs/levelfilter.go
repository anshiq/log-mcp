package logs

import "strings"

// levelAliases maps recognized severity tokens to their canonical level.
var levelAliases = map[string]string{
	"debug":         "debug",
	"trace":         "debug",
	"info":          "info",
	"informational": "info",
	"notice":        "info",
	"warn":          "warn",
	"warning":       "warn",
	"err":           "error",
	"error":         "error",
	"fatal":         "error",
	"critical":      "error",
	"panic":         "error",
}

// ParseLevel is a cheap heuristic that extracts a structured log level from a
// line. It recognizes, in order:
//
//   - "level=<lvl>" (slog text handler, zap console, journald), e.g.
//     `time=2024-01-01T00:00:00Z level=warn msg=slow`;
//   - `"level":"<lvl>"` (slog JSON handler, JSON loggers), e.g.
//     `{"ts":1,"level":"error","msg":"boom"}`;
//   - a leading bracketed, colon-suffixed or bare severity word, e.g.
//     `[WARN] ...`, `ERROR: ...`, `debug message`.
//
// It returns one of "debug", "info", "warn", "error", or "" when no level can
// be identified. It is intentionally cheap: it scans for the two structured
// forms first and then falls back to the first word of the line, so it may
// mis-classify plaintext lines that happen to start with a severity word.
func ParseLevel(line string) string {
	if i := strings.Index(line, "level="); i >= 0 {
		rest := line[i+len("level="):]
		if strings.HasPrefix(rest, `"`) {
			if v := quotedValue(rest); v != "" {
				if lv, ok := normalizeLevel(v); ok {
					return lv
				}
			}
		} else if v := leadingToken(rest); v != "" {
			if lv, ok := normalizeLevel(v); ok {
				return lv
			}
		}
	}
	if i := strings.Index(line, `"level"`); i >= 0 {
		rest := strings.TrimLeft(line[i+len(`"level"`):], " \t")
		if strings.HasPrefix(rest, ":") {
			rest = strings.TrimLeft(rest[1:], " \t")
			if strings.HasPrefix(rest, `"`) {
				if v := quotedValue(rest); v != "" {
					if lv, ok := normalizeLevel(v); ok {
						return lv
					}
				}
			} else if v := leadingToken(rest); v != "" {
				if lv, ok := normalizeLevel(v); ok {
					return lv
				}
			}
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "[") {
		if close := strings.IndexByte(trimmed, ']'); close > 0 {
			if lv, ok := normalizeLevel(trimmed[1:close]); ok {
				return lv
			}
		}
	}
	if i := strings.IndexByte(trimmed, ':'); i > 0 {
		if lv, ok := normalizeLevel(trimmed[:i]); ok {
			return lv
		}
	}
	if lv, ok := normalizeLevel(leadingToken(trimmed)); ok {
		return lv
	}
	return ""
}

// FilterByLevel returns entries whose parsed level equals level, preserving
// order. An empty level or "all" returns the entries unchanged; a requested
// level that is not a known severity also leaves the entries unchanged. In all
// other cases only entries whose ParseLevel equals the requested level (after
// alias normalization) are kept, so unknown-level lines are dropped alongside
// lines at other levels.
func FilterByLevel(entries []Entry, level string) []Entry {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" || level == "all" {
		return entries
	}
	canonical, ok := levelAliases[level]
	if !ok {
		return entries // unknown requested level: filter nothing
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if ParseLevel(e.Line) == canonical {
			out = append(out, e)
		}
	}
	return out
}

// normalizeLevel maps a raw severity token to its canonical level.
func normalizeLevel(s string) (string, bool) {
	lv, ok := levelAliases[strings.ToLower(strings.TrimSpace(s))]
	return lv, ok
}

// leadingToken returns the leading word of s, stopping at whitespace or a
// value delimiter (comma, closing brace/bracket, quote).
func leadingToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	end := len(s)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', ',', '}', ')', '"', ']':
			end = i
			goto done
		}
	}
done:
	return s[:end]
}

// quotedValue reads a double-quoted string at the start of s, returning the
// unquoted contents (or "" when s does not start with a well-formed quote).
func quotedValue(s string) string {
	if len(s) < 2 || s[0] != '"' {
		return ""
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // skip the escaped byte
		case '"':
			return s[1:i]
		}
	}
	return ""
}
