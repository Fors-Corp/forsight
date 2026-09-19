package model

import "strings"

// FallbackSeverity is the substring rule every collector without a
// structured level of its own falls back to: filelog when a tailed line
// carries no level, and the classify-preview endpoint when Forseer's model
// isn't trained enough yet to have an opinion. It lives here, not in
// filelog, so nothing that only wants the fallback rule — an HTTP handler
// among them — has to import a collector implementation to get it.
//
// It is exported because Forseer grades itself against it on the same
// stream, and a benchmark nobody can name is not a benchmark.
func FallbackSeverity(line string) LogSeverity {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "fatal") || strings.Contains(lower, "error") || strings.Contains(lower, "fail"):
		return LogSeverityError
	case strings.Contains(lower, "warn"):
		return LogSeverityWarn
	case strings.Contains(lower, "debug"):
		return LogSeverityDebug
	default:
		return LogSeverityInfo
	}
}
