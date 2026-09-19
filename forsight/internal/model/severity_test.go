package model

import "testing"

// FallbackSeverity is the benchmark Forseer's severity model grades itself
// against, and forseer's own tests carry a stand-in copy of this rule so
// that module stays dependency-free. Pinning the real rule here is what
// makes the two comparable: if this table has to change, the copy in
// forseer/severity_test.go has to change with it.
func TestFallbackSeverity_IsPinnedAsTheBenchmark(t *testing.T) {
	cases := []struct {
		line string
		want LogSeverity
	}{
		{"fatal: out of memory", LogSeverityError},
		{"could not open file: error 13", LogSeverityError},
		{"failed to connect", LogSeverityError},
		{"warn: retrying", LogSeverityWarn},
		{"debug: entering handler", LogSeverityDebug},
		{"request served in 4ms", LogSeverityInfo},

		// The blind spots, pinned deliberately: the rule is wrong on all
		// four, and those are the cases the model has to win to be worth
		// switching on.
		{"no errors reported during the sweep", LogSeverityError},
		{"error_rate 0 for checkout", LogSeverityError},
		{"recovered from the earlier failure", LogSeverityError},
		{"panic: nil map write", LogSeverityInfo},
	}

	for _, tc := range cases {
		if got := FallbackSeverity(tc.line); got != tc.want {
			t.Errorf("FallbackSeverity(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}
