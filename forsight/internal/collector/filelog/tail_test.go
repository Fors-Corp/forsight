package filelog

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

type memSink struct{ logs []model.LogEntry }

func (m *memSink) WriteLogs(_ context.Context, logs []model.LogEntry) error {
	m.logs = append(m.logs, logs...)
	return nil
}

// lockedSink is a Sink safe for concurrent use, for tests that read the
// accumulated logs from the test goroutine while Tail writes to it from a
// background goroutine.
type lockedSink struct {
	mu   sync.Mutex
	logs []model.LogEntry
}

func (s *lockedSink) WriteLogs(_ context.Context, logs []model.LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, logs...)
	return nil
}

func (s *lockedSink) snapshot() []model.LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.LogEntry, len(s.logs))
	copy(out, s.logs)
	return out
}

func waitForLogs(t *testing.T, sink *lockedSink, want int) []model.LogEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := sink.snapshot()
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d log lines, got %d: %+v", want, len(got), got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReadOnce_ClassifiesErrorLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	body := "ready to serve\nERROR dial tcp 10.0.0.1:5432\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	n, err := readOnce(context.Background(), path, "app.log", 0, sink, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("read zero bytes")
	}
	if len(sink.logs) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(sink.logs), sink.logs)
	}
	if sink.logs[0].Severity != model.LogSeverityInfo {
		t.Errorf("first severity = %s", sink.logs[0].Severity)
	}
	if sink.logs[1].Severity != model.LogSeverityError {
		t.Errorf("second severity = %s", sink.logs[1].Severity)
	}
	if sink.logs[1].Source != "app.log" {
		t.Errorf("source = %s", sink.logs[1].Source)
	}
}

func TestTail_StopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := Tail(ctx, path, &memSink{})
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
}

// TestTail_ResumesAfterTruncation guards against the tailer silently
// parking at a stale offset forever after log rotation: without
// resetOffsetOnTruncate, Seek-ing past a shrunk file's new EOF succeeds
// with a nil error, so the Scanner reads zero lines and the tailer never
// notices new content was written after the truncation.
func TestTail_ResumesAfterTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	// The post-rotation content must be shorter than the pre-rotation
	// offset for this to exercise the bug: resetOffsetOnTruncate only
	// resets when the file has strictly shrunk relative to the tracked
	// offset.
	if err := os.WriteFile(path, []byte("first\nsecond\nthird\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sink := &lockedSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Tail(ctx, path, sink) }()

	// Let the tailer catch up to the pre-rotation content before truncating
	// out from under it.
	waitForLogs(t, sink, 3)

	if err := os.WriteFile(path, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := waitForLogs(t, sink, 4)
	cancel()
	<-done

	if got[3].Message != "new" {
		t.Fatalf("post-truncation line missing/wrong, got: %+v", got)
	}
}

// TestReadOnce_CapsLinesPerCycle guards against the medium-severity finding
// alongside PR #44's OTLP maxLogsPerRequest cap: before this fix, readOnce
// buffered every line appended since the last poll into one unbounded
// slice before a single WriteLogs call, so a tight logging loop could push
// millions of lines through in one shot. A burst past maxLinesPerCycle must
// not block or crash — it is processed up to the cap, in flushChunkSize
// chunks, and the remainder is left for the next poll.
func TestReadOnce_CapsLinesPerCycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "burst.log")

	var buf bytes.Buffer
	total := maxLinesPerCycle + 500
	for i := 0; i < total; i++ {
		fmt.Fprintf(&buf, "line %d\n", i)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	sink := &memSink{}
	n1, err := readOnce(context.Background(), path, "burst.log", 0, sink, nil)
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if len(sink.logs) != maxLinesPerCycle {
		t.Fatalf("first cycle processed %d lines, want exactly the cap %d", len(sink.logs), maxLinesPerCycle)
	}
	if n1 >= int64(buf.Len()) {
		t.Fatalf("first cycle consumed the whole file (offset %d >= %d); want it to stop at the cap", n1, buf.Len())
	}

	n2, err := readOnce(context.Background(), path, "burst.log", n1, sink, nil)
	if err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	if len(sink.logs) != total {
		t.Fatalf("after second cycle got %d lines total, want %d", len(sink.logs), total)
	}
	if n2 != int64(buf.Len()) {
		t.Fatalf("second cycle offset = %d, want end of file %d", n2, buf.Len())
	}
}

// TestReadOnce_SkipsOversizedLine guards against the finding related to the
// per-cycle cap: a single line longer than the Scanner's maxLineBytes
// buffer used to return bufio.ErrTooLong fatally out of readOnce, which
// Tail then returned fatally too — cmd/run.go just logs "log tailer
// stopped" and the file goes permanently unwatched. The fix must skip past
// the offending line (not lose track of the stream) and keep tailing.
func TestReadOnce_SkipsOversizedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	huge := strings.Repeat("x", maxLineBytes+1024)
	body := "before\n" + huge + "\nafter\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	sink := &memSink{}
	n, err := readOnce(context.Background(), path, "big.log", 0, sink, nil)
	if err != nil {
		t.Fatalf("readOnce returned fatally instead of skipping the oversized line: %v", err)
	}
	if n != int64(len(body)) {
		t.Fatalf("offset = %d, want end of file %d (tailer should resume past the skipped line)", n, len(body))
	}
	if len(sink.logs) != 2 {
		t.Fatalf("got %d lines, want 2 (before/after, oversized line dropped): %+v", len(sink.logs), sink.logs)
	}
	if sink.logs[0].Message != "before" || sink.logs[1].Message != "after" {
		t.Fatalf("unexpected lines: %+v", sink.logs)
	}
}

// stubClassifier stands in for a warm Forseer severity model.
type stubClassifier struct {
	answer string
	ok     bool
	asked  []string
}

func (c *stubClassifier) ClassifySeverity(message string) (string, bool) {
	c.asked = append(c.asked, message)
	return c.answer, c.ok
}

func TestClassify_UsesTheSubstringRuleWithoutAClassifier(t *testing.T) {
	// The blind spot the trained model exists to fix: the word is present,
	// the meaning is not. Pinned here so a change to the rule is deliberate.
	severity, inferred := classify(nil, "no errors reported during the sweep")

	if severity != model.LogSeverityError {
		t.Errorf("got %q, want the rule's own (wrong) answer %q", severity, model.LogSeverityError)
	}
	if !inferred {
		t.Error("a tailed line's severity is always inferred, never declared")
	}
}

func TestClassify_PrefersAConfidentClassifier(t *testing.T) {
	stub := &stubClassifier{answer: "info", ok: true}

	severity, inferred := classify(stub, "no errors reported during the sweep")

	if severity != model.LogSeverityInfo {
		t.Errorf("got %q, want the classifier's answer %q", severity, model.LogSeverityInfo)
	}
	if !inferred {
		t.Error("severity stays inferred even when a model produced it, or the model would train on its own output")
	}
	if len(stub.asked) != 1 {
		t.Errorf("classifier asked %d times, want 1", len(stub.asked))
	}
}

func TestClassify_FallsBackWhenTheClassifierDeclines(t *testing.T) {
	stub := &stubClassifier{ok: false}

	// A line the substring rule does recognise, so "fell back" is
	// distinguishable from "found nothing and defaulted to info".
	severity, _ := classify(stub, "fatal: the payment service is unreachable")

	if severity != model.LogSeverityError {
		t.Errorf("got %q, want the fallback's %q", severity, model.LogSeverityError)
	}
}

func TestClassify_RefusesALevelTheAgentDoesNotDefine(t *testing.T) {
	// The model learns whatever strings the stream carries. A level outside
	// the agent's vocabulary must not reach the store.
	stub := &stubClassifier{answer: "SEVERE", ok: true}

	severity, _ := classify(stub, "everything is on fire")

	if severity != model.LogSeverityInfo {
		t.Errorf("got %q, want the fallback's %q for an unknown level", severity, model.LogSeverityInfo)
	}
}

func TestClassify_AcceptsAKnownLevelInAnyCasing(t *testing.T) {
	stub := &stubClassifier{answer: "  WARN  ", ok: true}

	severity, _ := classify(stub, "queue depth above the soft limit")

	if severity != model.LogSeverityWarn {
		t.Errorf("got %q, want %q", severity, model.LogSeverityWarn)
	}
}

func TestTailWith_MarksEveryTailedLineAsInferred(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("something happened\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}

	if _, err := readOnce(context.Background(), path, "app.log", 0, sink, nil); err != nil {
		t.Fatal(err)
	}

	if len(sink.logs) != 1 {
		t.Fatalf("got %d entries, want 1", len(sink.logs))
	}
	if !sink.logs[0].SeverityInferred {
		t.Error("a tailed entry was not marked as having an inferred severity")
	}
}
