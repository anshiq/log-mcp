// Log redaction: compiled regex rules applied to served log output
// (GetLogs/Search/Tail). Secrets never leave the daemon unmasked when a
// rule matches; env redaction (RedactEnv) is independent and stays.
package config

import (
	"regexp"
)

// Redactor masks configured patterns with ***.
type Redactor struct {
	res []*regexp.Regexp
}

// CompileRedactor compiles patterns; invalid ones are skipped (and warned
// about at Validate time by callers that check).
func CompileRedactor(patterns []string) *Redactor {
	r := &Redactor{}
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			r.res = append(r.res, re)
		}
	}
	return r
}

// Empty reports whether no rules are configured.
func (r *Redactor) Empty() bool { return r == nil || len(r.res) == 0 }

// Redact returns the line with all matches replaced by ***.
func (r *Redactor) Redact(line string) string {
	if r.Empty() {
		return line
	}
	for _, re := range r.res {
		line = re.ReplaceAllString(line, "***")
	}
	return line
}
