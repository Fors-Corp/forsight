package forseer

import (
	"strings"
	"testing"
)

// FuzzSeverityTokens targets severityTokens: the tokenizer that runs on
// every line offered to the severity model, both to Learn and to Classify.
// It cuts at 8192 bytes before ranging over runes, which can land the cut in
// the middle of a multi-byte rune — Go's range-over-string turns the
// trailing, now-invalid bytes into one or more utf8.RuneError runs rather
// than panicking, so this is a tripwire against that regressing, not a bug
// hunt.
func FuzzSeverityTokens(f *testing.F) {
	// A rune that straddles the 8192-byte cut point: "日" ("日") is a
	// 3-byte rune in UTF-8. Placed to start at byte 8190, message[:8192]
	// keeps its first two bytes and drops the third.
	straddling := strings.Repeat("x", 8190) + "日本語 error after the cut"

	seeds := []string{
		"",
		"   ",
		"a",
		"ok",
		"panic: nil map write in handler",
		"could not reach the database, giving up",
		"request 12345 took 42ms for user 987",
		strings.Repeat("word ", 500), // forces the severityMaxTokens cap
		strings.Repeat("1234567890 ", 2000),
		straddling,
		"\x00\x01\xff\xfe control and invalid bytes",
		"日本語のエラーメッセージ、詳細不明",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		toks := severityTokens(msg)

		// Bounded state: Learn/Classify both index class.counts by these
		// values, so a bucket outside the table would panic there instead of
		// here, and a token count above the per-line cap would let one
		// pathological line dominate a class — the exact failure the cap in
		// MODELS.md's contract exists to prevent.
		if len(toks) > severityMaxTokens {
			t.Fatalf("severityTokens(%q) returned %d tokens, want <= %d", msg, len(toks), severityMaxTokens)
		}
		for _, bucket := range toks {
			if bucket >= severityBuckets {
				t.Fatalf("severityTokens(%q) produced bucket %d, want < %d", msg, bucket, severityBuckets)
			}
		}

		// Idempotent / deterministic: the function has no state, and Learn
		// calls it once to predict and again implicitly on the same message
		// via the caller's own re-tokenization in tests; two calls on the
		// same input must never disagree.
		again := severityTokens(msg)
		if len(again) != len(toks) {
			t.Fatalf("severityTokens(%q) is not deterministic: %v then %v", msg, toks, again)
		}
		for i := range toks {
			if toks[i] != again[i] {
				t.Fatalf("severityTokens(%q) is not deterministic: %v then %v", msg, toks, again)
			}
		}
	})
}
