package mlaas

import (
	"bytes"
	"encoding/csv"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

// Dataset export: the store's raw stream turned into the CSV mlaas trains
// on. Everything here is a pure function of its inputs so the bucketing,
// formatting and minimums can be tested without a server.

const (
	// forecastTimeColumn and forecastValueColumn are the two columns of a
	// forecast dataset: "at,value".
	forecastTimeColumn  = "at"
	forecastValueColumn = "value"
	// severityTextColumn and severityLabelColumn are the two columns of the
	// classifier's dataset: "message,severity".
	severityTextColumn  = "message"
	severityLabelColumn = "severity"

	// forecastBucket is the resolution of a forecast series. The collectors
	// sample every few seconds; a Holt-Winters model on that cadence would
	// mostly learn sampling jitter, and one row a minute keeps two weeks of
	// history at a size mlaas re-reads comfortably on every retrain.
	forecastBucket = time.Minute
	// minForecastRows is the fewest buckets worth registering a dataset
	// for: below half an hour of history a forecast of the next hour is
	// noise, and mlaas would still need a holdout out of it.
	minForecastRows = 30
	// maxForecastRows caps the series at the newest 14 days (20160 minutes).
	// Older history does not help an hour-ahead forecast, and the whole
	// file is re-uploaded whenever it grows.
	maxForecastRows = 20160

	// minSeverityRows and minSeverityClasses gate the classifier's dataset:
	// naive Bayes on fewer than 50 lines, or on lines that all carry the
	// same level, would train a model that has nothing to learn.
	minSeverityRows    = 50
	minSeverityClasses = 2
	// maxSeverityRows keeps the newest 5000 declared lines. The feedback
	// loop keeps growing mlaas's own labelled corpus after that, so the
	// base file only has to seed the first champion.
	maxSeverityRows = 5000
	// maxMessageBytes truncates a message cell. Anything longer is a stack
	// trace or a dumped payload, and the level is decided by its first
	// lines; the same cap bounds what Predict proxies.
	maxMessageBytes = 4096

	// timeLayout is how the "at" column is written: RFC3339 in UTC to the
	// second, which is also the form the sync pass passes back to
	// GET /models/{name}/actual so the two match key for key.
	timeLayout = "2006-01-02T15:04:05Z"
)

// row is one data row of a CSV export, in header order. at is the bucket
// a forecast row stands for, so the sync pass can find the series' origin
// without parsing the cell back; it is zero for severity rows.
type row struct {
	at    time.Time
	cells []string
}

// forecastRows buckets a metric series into one-minute means, oldest
// first, newest maxForecastRows kept. Labels are ignored: one agent, one
// host, one series per metric name.
//
// The bucket now falls in is left out. It is still filling, so its mean
// would move on the next pass without changing the row count — and the row
// count is what decides whether to re-upload — and mlaas would label
// earlier predictions for that minute with a half-made value. Dropping it
// makes every exported row final the moment it is exported.
func forecastRows(metrics []model.Metric, now time.Time) []row {
	cutoff := now.UTC().Truncate(forecastBucket)
	type acc struct {
		sum float64
		n   int
	}
	buckets := map[time.Time]*acc{}
	for _, m := range metrics {
		if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
			continue
		}
		at := m.Timestamp.UTC().Truncate(forecastBucket)
		if !at.Before(cutoff) {
			continue
		}
		a := buckets[at]
		if a == nil {
			a = &acc{}
			buckets[at] = a
		}
		a.sum += m.Value
		a.n++
	}
	ats := make([]time.Time, 0, len(buckets))
	for at := range buckets {
		ats = append(ats, at)
	}
	slices.SortFunc(ats, func(a, b time.Time) int { return a.Compare(b) })
	if len(ats) > maxForecastRows {
		ats = ats[len(ats)-maxForecastRows:]
	}
	rows := make([]row, len(ats))
	for i, at := range ats {
		a := buckets[at]
		rows[i] = row{at: at, cells: []string{
			at.Format(timeLayout),
			strconv.FormatFloat(a.sum/float64(a.n), 'g', -1, 64),
		}}
	}
	return rows
}

// severityRows turns log entries into "message,severity" rows in time
// order, newest maxSeverityRows kept. Only lines whose source declared a
// level qualify: a level the agent inferred from the text is the agent's
// own guess, and training on it would teach mlaas to agree with Forseer's
// fallback rule rather than with the deployment.
func severityRows(logs []model.LogEntry) []row {
	kept := make([]model.LogEntry, 0, len(logs))
	for _, l := range logs {
		if l.SeverityInferred {
			continue
		}
		if strings.TrimSpace(l.Message) == "" || strings.TrimSpace(string(l.Severity)) == "" {
			continue
		}
		kept = append(kept, l)
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Timestamp.Before(kept[j].Timestamp) })
	if len(kept) > maxSeverityRows {
		kept = kept[len(kept)-maxSeverityRows:]
	}
	rows := make([]row, len(kept))
	for i, l := range kept {
		rows[i] = row{cells: []string{truncateMessage(l.Message), strings.TrimSpace(string(l.Severity))}}
	}
	return rows
}

// distinctLabels counts the different values in the last cell of each row
// — the severities of a classifier export.
func distinctLabels(rows []row) int {
	seen := map[string]struct{}{}
	for _, r := range rows {
		if len(r.cells) == 0 {
			continue
		}
		seen[r.cells[len(r.cells)-1]] = struct{}{}
	}
	return len(seen)
}

// truncateMessage trims a message cell to maxMessageBytes without cutting a
// UTF-8 sequence in half, which would make the CSV invalid text.
func truncateMessage(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxMessageBytes {
		return s
	}
	cut := maxMessageBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// writeCSV renders a header and rows the way mlaas's reader expects: every
// row with exactly the header's field count, cells quoted as needed
// (a message with a newline or a comma stays one cell).
func writeCSV(header []string, rows []row) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if err := w.Write(r.cells); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
