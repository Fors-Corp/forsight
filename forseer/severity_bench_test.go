package forseer

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkSeverityModel_Learn measures the training half of the ingest
// path: tokenize, predict-then-grade against the fallback, then update the
// hashed counts for the observed class. Four classes is the realistic
// cardinality for this model — info/warn/error/fatal is the usual OTLP
// severity set — so the benchmark trains all four rather than one.
func BenchmarkSeverityModel_Learn(b *testing.B) {
	m := newSeverityModel().withFallback(fallbackSeverityOf)
	lines := severityBenchLines()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		line := lines[i%len(lines)]
		m.Learn(line.message, line.severity)
	}
}

// BenchmarkSeverityModel_Classify measures the read half once the model is
// past severityMinTrained and actually answering, rather than declining on a
// cold model — the case that matters for steady-state ingest cost.
func BenchmarkSeverityModel_Classify(b *testing.B) {
	m := newSeverityModel()
	lines := severityBenchLines()
	for i := 0; i < severityMinTrained*4; i++ {
		line := lines[i%len(lines)]
		m.Learn(line.message, line.severity)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		line := lines[i%len(lines)]
		m.Classify(line.message)
	}
}

type severityBenchLine struct {
	message  string
	severity string
}

// severityBenchLines is a small, four-class vocabulary at the shape real
// OTLP severities take, used by both benchmarks above.
func severityBenchLines() []severityBenchLine {
	var out []severityBenchLine
	for i := 0; i < 25; i++ {
		out = append(out,
			severityBenchLine{fmt.Sprintf("request %d completed in %dms", i, i%50), "info"},
			severityBenchLine{fmt.Sprintf("health probe ok for shard %d", i), "info"},
			severityBenchLine{fmt.Sprintf("retrying upstream call after timeout, attempt %d", i), "warn"},
			severityBenchLine{fmt.Sprintf("queue depth %d above the soft limit", i), "warn"},
			severityBenchLine{fmt.Sprintf("could not reach the database, attempt %d, giving up", i), "error"},
			severityBenchLine{"panic: nil map write in handler", "fatal"},
		)
	}
	return out
}

// fallbackSeverityOf is a small stand-in for the substring rule Forseer
// scores itself against, sized to keep this benchmark self-contained rather
// than reaching into the file-tail collector's package.
func fallbackSeverityOf(message string) string {
	switch {
	case containsAny(message, "panic", "fatal"):
		return "fatal"
	case containsAny(message, "error", "could not", "failed"):
		return "error"
	case containsAny(message, "retry", "retrying", "timeout", "above the"):
		return "warn"
	default:
		return "info"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
