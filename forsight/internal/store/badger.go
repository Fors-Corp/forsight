package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// BadgerStore is a Store implementation backed by a single embedded Badger
// key-value database, for data that survives a process restart. It is a
// drop-in swap for MemoryStore: same interface, same query semantics
// (time-range + exact-label/attribute match, retention-based pruning) —
// selected with `forsight run --store badger --data-dir <dir>` instead of
// the in-memory default.
//
// # Key encoding
//
// Every record is stored under a key of the form
//
//	<type byte> <name length, uint16 BE> <name bytes> <timestamp, uint64 BE nanoseconds> <sequence, uint64 BE>
//
// "name" is the field callers actually filter by in the common case — a
// metric's Name, a span's Service, a log entry's Source — so a query that
// specifies one becomes a Badger prefix scan (all data for that
// name/service/source) that can additionally Seek() straight to a Since
// timestamp instead of walking every older key first, because the
// big-endian timestamp encoding makes byte order match chronological order.
// Everything else the query can filter on (labels, TraceID, Severity) has
// no useful key-scan structure — MemoryStore doesn't index them either —
// so those are applied after decoding, via the exact same matchesMetric /
// matchesSpan / matchesLog predicates memory.go uses. Sharing those
// predicates, rather than re-deriving equivalent logic here, is what makes
// "identical query semantics" a property of the code and not just a claim.
//
// The name is length-prefixed rather than separated by a delimiter byte
// (e.g. a single 0x00). A delimiter can't be distinguished from that same
// byte occurring inside the name/service/source string itself, and those
// strings are attacker-influenced (OTLP resource/service attributes) —
// this package already carries one fix for a hostile-payload memory issue
// on that same ingest path (see internal/collector/otlp). A length prefix
// makes every encoded key prefix-free: two distinct (length, bytes) pairs
// can never produce a key where one is a byte-prefix of the other, so a
// scan for name "cpu" can never sweep in a "cpu2" or a name that happens to
// contain a NUL byte. The length is capped (maxKeyNameLen) so a
// pathologically long name can't overflow the uint16 field; the record's
// full, untruncated name is still preserved in the JSON value, so a
// truncated key only ever costs some scan precision for that one
// pathological name, never data.
//
// A query with no name/service/source filter (MetricQuery{}, "give me
// everything") falls back to a full scan of that record type's keyspace.
// That's the same cost MemoryStore already pays for an unscoped query, and
// the less common case in practice — the dashboard and API almost always
// know which metric, service, or source they want.
//
// The trailing sequence number is a per-process, in-memory atomic counter
// (it does not persist across restarts). It exists only to keep keys
// unique when a single write batch contains several records sharing the
// exact same name and nanosecond timestamp — e.g. one collector tick
// producing host.cpu.percent for several containers at once. It is not a
// global ordering guarantee, and doesn't need to be one: a restart
// producing the exact same nanosecond timestamp and sequence value as a
// still-live pre-restart entry is not a realistic collision.
//
// The value is the record itself (model.Metric / model.Span / model.LogEntry),
// JSON-encoded in full — labels/attributes included — so a read never
// reconstructs anything from the key; the key exists purely to make range
// scans cheap.
//
// # Retention
//
// Every write sets the entry's Badger TTL (Entry.WithTTL) to the store's
// configured retention, anchored to wall-clock write time — not to
// whatever Timestamp field the payload claims, so a writer can't extend its
// own data's lifetime by lying about when it was collected (the same
// hostile-input concern MemoryStore's element cap defends against, closed
// here a different way). Badger checks expiry at read time in both Get and
// iteration (see badger's isDeletedOrExpired, checked against the real
// clock on every read) as well as during compaction, so an expired record
// stops being returned by queries exactly at the retention boundary without
// this store running any sweep/prune loop of its own — no
// pruneMetricsLocked/pruneSpansLocked/pruneLogsLocked equivalent exists
// here because Badger's TTL already is that mechanism.
//
// This is also why BadgerStore has no MemoryStore-style element cap: the
// element cap exists because MemoryStore's age-based pruning trusts the
// payload's own Timestamp, which an adversarial writer controls; Badger's
// TTL trusts only the local write clock, so the same attack (a far-future
// timestamp meant to dodge pruning) has no effect on when the entry
// actually expires.
//
// One deliberate omission: this store never calls DB.RunValueLogGC.
// Badger only moves a value into the separate value log (the thing
// RunValueLogGC reclaims) once it exceeds Options.ValueThreshold, which
// defaults to 1 MiB; a JSON-encoded Metric/Span/LogEntry is a few hundred
// bytes, so every value here lives inline in the LSM tree and is reclaimed
// by Badger's ordinary compaction, which already drops expired entries
// (see levels.go's own isDeletedOrExpired check). A GC loop would be
// exercising a code path this store's data never touches.
type BadgerStore struct {
	db        *badger.DB
	retention time.Duration
	seq       atomic.Uint64
}

const (
	metricKeyType byte = 'm'
	spanKeyType   byte = 's'
	logKeyType    byte = 'l'

	// maxKeyNameLen bounds how much of a name/service/source goes into the
	// key itself. It exists to keep the uint16 length prefix from
	// overflowing on a pathological input, not because legitimate names
	// approach this length.
	maxKeyNameLen = 4096
)

// NewBadgerStore opens (or creates) a Badger database at dir and returns a
// Store backed by it. The caller must call Close when done — see the
// shutdown path in cmd/run.go, which hooks it into the same place the HTTP
// server is shut down.
func NewBadgerStore(dir string, retention time.Duration) (*BadgerStore, error) {
	// Badger's own logger defaults to INFO, which logs routine internals
	// (compaction, memtable flushes) on every run; WARNING keeps real
	// problems visible on stderr without that noise.
	opts := badger.DefaultOptions(dir).WithLoggingLevel(badger.WARNING)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger database at %q: %w", dir, err)
	}
	return &BadgerStore{db: db, retention: retention}, nil
}

// Close releases the Badger database's on-disk lock. It is safe to call
// more than once.
func (s *BadgerStore) Close() error {
	return s.db.Close()
}

// ttl is the retention duration to hand to Entry.WithTTL. Badger computes
// an entry's expiry as time.Now().Add(dur) cast to a uint64 Unix
// timestamp; a sufficiently negative dur would push that before the Unix
// epoch and wrap around to a huge, effectively-never-expiring value, which
// is the opposite of a negative retention's intent. Clamping at zero
// (immediate expiry, matching MemoryStore's behavior when retention <= 0
// prunes on the very next read) avoids relying on any caller never passing
// an unusual --retention value.
func (s *BadgerStore) ttl() time.Duration {
	if s.retention < 0 {
		return 0
	}
	return s.retention
}

func (s *BadgerStore) nextSeq() uint64 {
	return s.seq.Add(1)
}

// encodeKey builds the full <type><nameLen><name><ts><seq> key described in
// BadgerStore's doc comment.
func encodeKey(typ byte, name string, ts time.Time, seq uint64) []byte {
	nb := truncatedKeyName(name)
	buf := make([]byte, 0, 1+2+len(nb)+8+8)
	buf = append(buf, typ)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(nb)))
	buf = append(buf, nb...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(ts.UnixNano()))
	buf = binary.BigEndian.AppendUint64(buf, seq)
	return buf
}

// encodeNamePrefix builds the <type><nameLen><name> prefix that scopes a
// scan to one name/service/source, with no timestamp/sequence suffix.
func encodeNamePrefix(typ byte, name string) []byte {
	nb := truncatedKeyName(name)
	buf := make([]byte, 0, 1+2+len(nb))
	buf = append(buf, typ)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(nb)))
	buf = append(buf, nb...)
	return buf
}

func truncatedKeyName(name string) []byte {
	nb := []byte(name)
	if len(nb) > maxKeyNameLen {
		nb = nb[:maxKeyNameLen]
	}
	return nb
}

// keyTimestamp extracts the timestamp encoded in a key built by encodeKey.
// It lets a full-keyspace scan (no name/service/source filter) reject a
// key that fails a Since filter before paying for a value decode.
func keyTimestamp(key []byte) time.Time {
	nameLen := int(binary.BigEndian.Uint16(key[1:3]))
	off := 3 + nameLen
	ns := binary.BigEndian.Uint64(key[off : off+8])
	return time.Unix(0, int64(ns))
}

// scanBounds computes the iterator prefix and the seek key a reverse
// (newest-first) scan starts from: the largest key in the window. For a
// scan scoped to one name/service/source that is the key at Before when it
// is set, else the prefix followed by the highest possible timestamp and
// sequence. filterValue == "" means "any name" — the full-keyspace-scan
// case described in the doc comment — where the name sits between the type
// byte and the timestamp, so time has no key order across names and the
// seek can only be "past every key of this type": no real key has a name
// length of 0xFFFF, so <type> 0xFF 0xFF is beyond all of them.
func scanBounds(typ byte, filterValue string, before time.Time) (prefix, seek []byte) {
	if filterValue == "" {
		prefix = []byte{typ}
		return prefix, []byte{typ, 0xFF, 0xFF}
	}
	prefix = encodeNamePrefix(typ, filterValue)
	if before.IsZero() {
		seek = append(append([]byte{}, prefix...), maxKeySuffix...)
		return prefix, seek
	}
	return prefix, encodeKey(typ, filterValue, before, 0)
}

// maxKeySuffix is the highest <timestamp><sequence> a key can carry.
var maxKeySuffix = bytes.Repeat([]byte{0xFF}, 16)

// scanNewestFirst walks one record type's keyspace backwards from the top
// of the [since, before) window and stops at limit, so a query for the
// newest N reads N keys and not the whole history. The result is reversed
// on the way out, oldest-first, which is what every consumer expects. Within
// one name the key's timestamp orders the walk, so a key older than since
// ends the scan; across names (the unscoped case) it only skips.
func scanNewestFirst[T any](txn *badger.Txn, typ byte, filterValue string, since, before time.Time, limit int,
	decode func([]byte) (T, error), match func(T) bool) ([]T, error) {
	out := make([]T, 0)
	prefix, seek := scanBounds(typ, filterValue, before)
	scoped := filterValue != ""
	iopts := badger.DefaultIteratorOptions
	iopts.Prefix = prefix
	iopts.Reverse = true
	it := txn.NewIterator(iopts)
	defer it.Close()
	for it.Seek(seek); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		ts := keyTimestamp(item.Key())
		if !before.IsZero() && !ts.Before(before) {
			continue
		}
		if !since.IsZero() && ts.Before(since) {
			if scoped {
				break
			}
			continue
		}
		var rec T
		var err error
		if verr := item.Value(func(val []byte) error { rec, err = decode(val); return err }); verr != nil {
			return nil, verr
		}
		if !match(rec) {
			continue
		}
		out = append(out, rec)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *BadgerStore) WriteMetrics(_ context.Context, metrics []model.Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	wb := s.db.NewWriteBatch()
	defer wb.Cancel()
	for _, m := range metrics {
		val, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("encode metric %q: %w", m.Name, err)
		}
		key := encodeKey(metricKeyType, m.Name, m.Timestamp, s.nextSeq())
		if err := wb.SetEntry(badger.NewEntry(key, val).WithTTL(s.ttl())); err != nil {
			return fmt.Errorf("write metric %q: %w", m.Name, err)
		}
	}
	if err := wb.Flush(); err != nil {
		return fmt.Errorf("flush metrics batch: %w", err)
	}
	return nil
}

func (s *BadgerStore) QueryMetrics(_ context.Context, q MetricQuery) ([]model.Metric, error) {
	var out []model.Metric
	err := s.db.View(func(txn *badger.Txn) error {
		var err error
		out, err = scanNewestFirst(txn, metricKeyType, q.Name, q.Since, q.Before, q.Limit,
			func(val []byte) (model.Metric, error) {
				var rec model.Metric
				if err := json.Unmarshal(val, &rec); err != nil {
					return rec, fmt.Errorf("decode metric: %w", err)
				}
				return rec, nil
			},
			func(rec model.Metric) bool { return matchesMetric(rec, q) })
		return err
	})
	if out == nil {
		out = make([]model.Metric, 0)
	}
	return out, err
}

func (s *BadgerStore) WriteSpans(_ context.Context, spans []model.Span) error {
	if len(spans) == 0 {
		return nil
	}
	wb := s.db.NewWriteBatch()
	defer wb.Cancel()
	for _, sp := range spans {
		val, err := json.Marshal(sp)
		if err != nil {
			return fmt.Errorf("encode span %q: %w", sp.TraceID, err)
		}
		key := encodeKey(spanKeyType, sp.Service, sp.Start, s.nextSeq())
		if err := wb.SetEntry(badger.NewEntry(key, val).WithTTL(s.ttl())); err != nil {
			return fmt.Errorf("write span %q: %w", sp.TraceID, err)
		}
	}
	if err := wb.Flush(); err != nil {
		return fmt.Errorf("flush spans batch: %w", err)
	}
	return nil
}

func (s *BadgerStore) QuerySpans(_ context.Context, q SpanQuery) ([]model.Span, error) {
	var out []model.Span
	err := s.db.View(func(txn *badger.Txn) error {
		var err error
		out, err = scanNewestFirst(txn, spanKeyType, q.Service, q.Since, q.Before, q.Limit,
			func(val []byte) (model.Span, error) {
				var rec model.Span
				if err := json.Unmarshal(val, &rec); err != nil {
					return rec, fmt.Errorf("decode span: %w", err)
				}
				return rec, nil
			},
			func(rec model.Span) bool { return matchesSpan(rec, q) })
		return err
	})
	if out == nil {
		out = make([]model.Span, 0)
	}
	return out, err
}

func (s *BadgerStore) WriteLogs(_ context.Context, logs []model.LogEntry) error {
	if len(logs) == 0 {
		return nil
	}
	wb := s.db.NewWriteBatch()
	defer wb.Cancel()
	for _, l := range logs {
		val, err := json.Marshal(l)
		if err != nil {
			return fmt.Errorf("encode log entry from %q: %w", l.Source, err)
		}
		key := encodeKey(logKeyType, l.Source, l.Timestamp, s.nextSeq())
		if err := wb.SetEntry(badger.NewEntry(key, val).WithTTL(s.ttl())); err != nil {
			return fmt.Errorf("write log entry from %q: %w", l.Source, err)
		}
	}
	if err := wb.Flush(); err != nil {
		return fmt.Errorf("flush logs batch: %w", err)
	}
	return nil
}

func (s *BadgerStore) QueryLogs(_ context.Context, q LogQuery) ([]model.LogEntry, error) {
	var out []model.LogEntry
	err := s.db.View(func(txn *badger.Txn) error {
		var err error
		out, err = scanNewestFirst(txn, logKeyType, q.Source, q.Since, q.Before, q.Limit,
			func(val []byte) (model.LogEntry, error) {
				var rec model.LogEntry
				if err := json.Unmarshal(val, &rec); err != nil {
					return rec, fmt.Errorf("decode log entry: %w", err)
				}
				return rec, nil
			},
			func(rec model.LogEntry) bool { return matchesLog(rec, q) })
		return err
	})
	if out == nil {
		out = make([]model.LogEntry, 0)
	}
	return out, err
}
