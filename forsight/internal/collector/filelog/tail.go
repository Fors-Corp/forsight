// Package filelog tails log files into model.LogEntry values so a default
// install can watch application logs without an OTLP SDK.
package filelog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// Sink is the write side of the store, declared here so this package does
// not import store.
type Sink interface {
	WriteLogs(ctx context.Context, logs []model.LogEntry) error
}

// Classifier gives a tailed line the severity this deployment would give it.
//
// A tailed file carries no level, so one has to be worked out from the text.
// model.FallbackSeverity is the rule that has always done it, and it is
// wrong whenever the word and the meaning disagree ("no errors reported").
// Forseer's severity model, trained on the levels the OTLP half of the same
// stream declares, does better once it has seen enough — and says so by
// returning false until then, which is why this is an interface the agent
// injects rather than a dependency this package takes.
type Classifier interface {
	// ClassifySeverity returns the level for message, and false when the
	// caller should use its own fallback instead.
	ClassifySeverity(message string) (string, bool)
}

const (
	// maxLineBytes bounds a single line's memory footprint, matching the
	// Scanner token cap this package has always used. A line longer than
	// this cannot come from a well-behaved logger, and is skipped rather
	// than buffered — see readOnce.
	maxLineBytes = 1024 * 1024

	// maxLinesPerCycle bounds how many lines a single readOnce call will
	// process, mirroring the order of magnitude of otlp.maxLogsPerRequest
	// (200_000): the same "unbounded burst in one shot" bug class PR #44
	// fixed on the OTLP ingest path also applied here, since readOnce used
	// to buffer every line appended since the last poll into one slice
	// before a single WriteLogs call. A tight logging loop can produce
	// millions of lines between two 400ms polls; past this cap the rest is
	// left for the next poll rather than growing this cycle's buffer or
	// blocking on one giant write.
	maxLinesPerCycle = 200_000

	// flushChunkSize bounds how many entries accumulate before a WriteLogs
	// call, so a single cycle's work reaches the sink (and the log-miner
	// mutex behind it) in bounded chunks instead of one call sized to
	// maxLinesPerCycle.
	flushChunkSize = 2_000
)

// Tail follows path until ctx is cancelled. New lines become log entries
// with Source set to the file's base name. Missing files are retried.
//
// Severity is worked out from the line text by the substring rule. Use
// TailWith to offer a trained classifier the first refusal.
func Tail(ctx context.Context, path string, sink Sink) error {
	return TailWith(ctx, path, sink, nil)
}

// TailWith is Tail with a classifier consulted before the substring rule.
// A nil classifier, or one that declines a given line, leaves the rule in
// charge — so the tailer behaves identically until a model is actually
// better than it.
func TailWith(ctx context.Context, path string, sink Sink, classifier Classifier) error {
	source := filepath.Base(path)
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		offset = resetOffsetOnTruncate(path, offset)
		n, err := readOnce(ctx, path, source, offset, sink, classifier)
		if err != nil && ctx.Err() == nil && !os.IsNotExist(err) {
			return err
		}
		if n > offset {
			offset = n
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
}

// resetOffsetOnTruncate detects log rotation/truncation in place (e.g.
// logrotate's copytruncate, or an app truncating its own log file): if the
// file at path is now smaller than the offset we last read up to, that
// offset points past the new EOF. Seeking there succeeds silently — Seek
// past EOF is not itself an error — so the Scanner then reads zero lines
// forever, and left alone the tailer would park at the stale offset for
// the rest of the process's life with no rotation detected. Resuming from
// 0 re-reads the new file from its start. A stat error (including a
// mid-rotation gap where the file briefly doesn't exist) is left for
// readOnce, which already retries on os.IsNotExist.
func resetOffsetOnTruncate(path string, offset int64) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return offset
	}
	if info.Size() < offset {
		return 0
	}
	return offset
}

func readOnce(ctx context.Context, path, source string, offset int64, sink Sink, classifier Classifier) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer func() { _ = f.Close() }()

	var lines []model.LogEntry
	read := offset
	now := time.Now()
	processed := 0
	capped := false

	flush := func() error {
		if len(lines) == 0 {
			return nil
		}
		err := sink.WriteLogs(ctx, lines)
		lines = lines[:0]
		return err
	}

	// Each pass of this loop scans from the current `read` offset with a
	// fresh Scanner until either it hits real EOF, hits the per-cycle line
	// cap, hits an unrecoverable error, or hits an oversized line — in
	// which case it skips that line and loops back to keep tailing, all
	// within this one call, rather than returning fatally.
	for {
		if _, err := f.Seek(read, io.SeekStart); err != nil {
			if ferr := flush(); ferr != nil {
				return offset, ferr
			}
			return offset, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

		for sc.Scan() {
			text := sc.Text()
			read += int64(len(text) + 1)
			if strings.TrimSpace(text) != "" {
				severity, inferred := classify(classifier, text)
				lines = append(lines, model.LogEntry{
					Timestamp:        now,
					Severity:         severity,
					Source:           source,
					Message:          text,
					SeverityInferred: inferred,
				})
				if len(lines) >= flushChunkSize {
					if err := flush(); err != nil {
						return offset, err
					}
				}
			}
			processed++
			if processed >= maxLinesPerCycle {
				capped = true
				break
			}
		}
		if capped {
			break
		}

		scanErr := sc.Err()
		if scanErr == nil {
			// Reached real EOF cleanly for this pass — nothing more to
			// read right now.
			break
		}
		if !errors.Is(scanErr, bufio.ErrTooLong) {
			if ferr := flush(); ferr != nil {
				return offset, ferr
			}
			return offset, scanErr
		}

		// A single line exceeded maxLineBytes with no newline found within
		// it. `read` is still exactly the byte offset that line started
		// at, since it only advances after a line is fully scanned.
		// Re-seek the same fd there (discarding whatever the abandoned
		// Scanner had buffered ahead of it — we don't need it, we're
		// about to skip past that region a different way) and skip raw
		// bytes up to the next newline, rather than returning fatally and
		// leaving this file unwatched for the rest of the process's life.
		skipped, serr := skipLine(f, read)
		if serr != nil {
			if ferr := flush(); ferr != nil {
				return offset, ferr
			}
			return offset, serr
		}
		if skipped == 0 {
			// The oversized line hasn't reached a newline yet (we hit EOF
			// scanning for one) — leave the offset where it is and retry
			// once more data has been appended on a later poll.
			break
		}
		read += skipped
		slog.Default().Warn("filelog: skipped oversized log line", "path", path, "limit_bytes", maxLineBytes)
		// Loop again: keep tailing whatever comes after the skipped line.
	}

	if capped {
		slog.Default().Warn("filelog: per-cycle line cap reached, deferring remainder to next poll", "path", path, "cap", maxLinesPerCycle)
	}

	if err := flush(); err != nil {
		return offset, err
	}
	return read, nil
}

// skipLine reads f, already open on the tailed file, starting at start and
// looking for the next '\n'. It returns how many bytes (including the
// newline) to advance past. It reads in small fixed chunks so a
// pathologically long line costs O(1) memory rather than O(line length) —
// the exact problem maxLineBytes exists to avoid. Returns (0, nil) if EOF
// is reached with no newline found: the line has not finished arriving
// yet, and the caller should retry on a later poll once more data exists.
func skipLine(f *os.File, start int64) (int64, error) {
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, err
	}
	buf := make([]byte, 32*1024)
	var scanned int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if i := bytes.IndexByte(buf[:n], '\n'); i >= 0 {
				return scanned + int64(i) + 1, nil
			}
			scanned += int64(n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, nil
			}
			return 0, err
		}
	}
}

// classify asks the trained model first and falls back to the substring
// rule. The bool is true in both cases: a tailed line never carries a level
// the source declared, so the severity is always inferred, however good the
// thing that inferred it. That is what keeps the model from training on its
// own output.
func classify(classifier Classifier, line string) (model.LogSeverity, bool) {
	if classifier != nil {
		if severity, ok := classifier.ClassifySeverity(line); ok {
			if known, valid := knownSeverity(severity); valid {
				return known, true
			}
		}
	}
	return model.FallbackSeverity(line), true
}

// knownSeverity maps a model's answer onto the agent's vocabulary, refusing
// anything outside it. The model learns whatever strings the stream carries,
// and a level this agent does not define must not reach the store.
func knownSeverity(severity string) (model.LogSeverity, bool) {
	switch model.LogSeverity(strings.ToLower(strings.TrimSpace(severity))) {
	case model.LogSeverityDebug:
		return model.LogSeverityDebug, true
	case model.LogSeverityInfo:
		return model.LogSeverityInfo, true
	case model.LogSeverityWarn:
		return model.LogSeverityWarn, true
	case model.LogSeverityError:
		return model.LogSeverityError, true
	default:
		return "", false
	}
}
