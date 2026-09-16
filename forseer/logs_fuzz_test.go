package forseer

import (
	"strings"
	"testing"
)

// FuzzTemplateOf targets templateOf: the four-regex chain that every raw log
// line passes through before it can join a cluster. It is on the ingest
// path, so the only thing worth asserting is safety — no panic, and a
// well-formed answer — never a claim about what the "right" template is for
// arbitrary bytes.
func FuzzTemplateOf(f *testing.F) {
	// Seeds mirror the table tests: the four substitution kinds, one at a
	// time and combined, plus the edge cases those tests already care about
	// (empty, whitespace-only, no placeholders at all).
	seeds := []string{
		"",
		"   ",
		"\t\n  \t",
		"plain message with no identifiers",
		"request 550e8400-e29b-41d4-a716-446655440000 from 10.0.0.4 took 12.4ms",
		"user 1 login failed",
		"0xdeadbeef error code 42",
		"balance is 12.50 after 3 retries",
		"550e8400-e29b-41d4-a716-446655440000",
		"10.0.0.4",
		"日本語のログ 550e8400-e29b-41d4-a716-446655440000 エラー",
		"\x00\x01binary\xffgarbage",
		strings.Repeat("10.0.0.4 ", 200),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		got := templateOf(msg)

		// Idempotent: every placeholder templateOf can introduce is "<*>",
		// which contains none of the shapes the four regexes match, so a
		// second pass over the result must be a no-op. A template that kept
		// moving on its own output could never settle into a stable cluster
		// key.
		again := templateOf(got)
		if again != got {
			t.Fatalf("templateOf is not idempotent: templateOf(%q) = %q, templateOf(that) = %q", msg, got, again)
		}

		// Fields are joined with a single space and nothing else, so the
		// result never carries leading/trailing whitespace or run of blanks
		// that Fields() would have collapsed.
		if got != strings.TrimSpace(got) {
			t.Fatalf("templateOf(%q) = %q has leading/trailing whitespace", msg, got)
		}
		if strings.Contains(got, "  ") {
			t.Fatalf("templateOf(%q) = %q contains a double space", msg, got)
		}

		// Blank (after trimming) input maps to the empty template, and only
		// blank input does: observeOneLocked relies on "" meaning "nothing
		// to cluster on".
		if strings.TrimSpace(msg) == "" && got != "" {
			t.Fatalf("blank input produced non-empty template %q", got)
		}
	})
}
