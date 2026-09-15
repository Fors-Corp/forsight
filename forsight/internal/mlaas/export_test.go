package mlaas

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// base is a fixed instant well inside a minute, so bucket boundaries in
// these tests are unambiguous.
var base = time.Date(2026, 9, 1, 10, 30, 42, 0, time.UTC)

func TestForecastRows_BucketsByUTCMinuteAndAverages(t *testing.T) {
	loc := time.FixedZone("plus2", 2*3600)
	metrics := []model.Metric{
		// Two samples in 10:27 (mean 15), one in 10:28, arriving out of
		// order and in a non-UTC zone.
		{Name: "host.cpu.percent", Value: 30, Timestamp: base.Add(-2 * time.Minute).In(loc)},
		{Name: "host.cpu.percent", Value: 10, Timestamp: base.Add(-3 * time.Minute)},
		{Name: "host.cpu.percent", Value: 20, Timestamp: base.Add(-3*time.Minute - 20*time.Second)},
		// The minute now falls in is still filling: left out.
		{Name: "host.cpu.percent", Value: 99, Timestamp: base},
		// Non-finite values would break mlaas's CSV parser: left out.
		{Name: "host.cpu.percent", Value: math.NaN(), Timestamp: base.Add(-4 * time.Minute)},
		{Name: "host.cpu.percent", Value: math.Inf(1), Timestamp: base.Add(-5 * time.Minute)},
	}
	rows := forecastRows(metrics, base)
	want := [][]string{
		{"2026-09-01T10:27:00Z", "15"},
		{"2026-09-01T10:28:00Z", "30"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, r := range rows {
		if !reflect.DeepEqual(r.cells, want[i]) {
			t.Errorf("row %d = %v, want %v", i, r.cells, want[i])
		}
		if r.at.Location() != time.UTC || r.at.Format(timeLayout) != want[i][0] {
			t.Errorf("row %d at = %v, want the UTC bucket %s", i, r.at, want[i][0])
		}
	}
}

func TestForecastRows_FormatsValuesShortest(t *testing.T) {
	metrics := []model.Metric{
		{Value: 12.5, Timestamp: base.Add(-2 * time.Minute)},
		{Value: 1.0 / 3, Timestamp: base.Add(-1 * time.Minute)},
	}
	rows := forecastRows(metrics, base)
	if got := rows[0].cells[1]; got != "12.5" {
		t.Errorf("12.5 rendered as %q", got)
	}
	if got := rows[1].cells[1]; got != "0.3333333333333333" {
		t.Errorf("1/3 rendered as %q, want strconv 'g' -1", got)
	}
}

func TestForecastRows_KeepsNewestCap(t *testing.T) {
	metrics := make([]model.Metric, 0, maxForecastRows+10)
	for i := 0; i < maxForecastRows+10; i++ {
		metrics = append(metrics, model.Metric{Value: float64(i), Timestamp: base.Add(-time.Duration(i+1) * time.Minute)})
	}
	rows := forecastRows(metrics, base)
	if len(rows) != maxForecastRows {
		t.Fatalf("got %d rows, want the cap %d", len(rows), maxForecastRows)
	}
	if last := rows[len(rows)-1]; last.cells[1] != "0" {
		t.Errorf("newest row = %v, want the most recent minute (value 0)", last.cells)
	}
	if first := rows[0]; first.cells[1] != "20159" {
		t.Errorf("oldest kept row = %v, want value 20159", first.cells)
	}
}

func TestSeverityRows_KeepsDeclaredLinesInTimeOrder(t *testing.T) {
	logs := []model.LogEntry{
		{Timestamp: base.Add(2 * time.Second), Severity: "error", Message: "later line"},
		{Timestamp: base, Severity: "info", Message: "  first line  "},
		{Timestamp: base.Add(time.Second), Severity: "warn", Message: "guessed", SeverityInferred: true},
		{Timestamp: base.Add(time.Second), Severity: "", Message: "no level"},
		{Timestamp: base.Add(time.Second), Severity: "info", Message: "   "},
	}
	rows := severityRows(logs)
	want := [][]string{{"first line", "info"}, {"later line", "error"}}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i := range want {
		if !reflect.DeepEqual(rows[i].cells, want[i]) {
			t.Errorf("row %d = %v, want %v", i, rows[i].cells, want[i])
		}
	}
	if got := distinctLabels(rows); got != 2 {
		t.Errorf("distinctLabels = %d, want 2", got)
	}
}

func TestSeverityRows_TruncatesAndCaps(t *testing.T) {
	// A message of 4096 bytes minus one, then a 3-byte rune straddling the
	// cut: the cut must land before the rune, not inside it.
	long := strings.Repeat("a", maxMessageBytes-1) + "€" + strings.Repeat("b", 10)
	logs := make([]model.LogEntry, 0, maxSeverityRows+5)
	for i := 0; i < maxSeverityRows+5; i++ {
		logs = append(logs, model.LogEntry{Timestamp: base.Add(time.Duration(i) * time.Second), Severity: "info", Message: long})
	}
	rows := severityRows(logs)
	if len(rows) != maxSeverityRows {
		t.Fatalf("got %d rows, want the cap %d", len(rows), maxSeverityRows)
	}
	msg := rows[0].cells[0]
	if len(msg) != maxMessageBytes-1 || !strings.HasSuffix(msg, "a") {
		t.Errorf("message truncated to %d bytes ending %q; want %d bytes on the rune boundary", len(msg), msg[len(msg)-1:], maxMessageBytes-1)
	}
}

func TestEnough_Minimums(t *testing.T) {
	forecast, classifier := managed[0], managed[3]
	mk := func(n int, labels ...string) []row {
		rows := make([]row, n)
		for i := range rows {
			rows[i] = row{cells: []string{"m", labels[i%len(labels)]}}
		}
		return rows
	}
	if forecast.enough(mk(minForecastRows-1, "x")) {
		t.Error("29 buckets counted as enough for a forecast")
	}
	if !forecast.enough(mk(minForecastRows, "x")) {
		t.Error("30 buckets not counted as enough for a forecast")
	}
	if classifier.enough(mk(minSeverityRows, "info")) {
		t.Error("50 lines of one level counted as enough for the classifier")
	}
	if classifier.enough(mk(minSeverityRows-1, "info", "error")) {
		t.Error("49 lines counted as enough for the classifier")
	}
	if !classifier.enough(mk(minSeverityRows, "info", "error")) {
		t.Error("50 lines of two levels not counted as enough")
	}
}

func TestWriteCSV_QuotesAwkwardCells(t *testing.T) {
	out, err := writeCSV([]string{"message", "severity"}, []row{
		{cells: []string{"a, comma", "info"}},
		{cells: []string{"two\nlines", "error"}},
		{cells: []string{`with "quotes"`, "warn"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("mlaas's reader would fail on this file: %v", err)
	}
	want := [][]string{{"message", "severity"}, {"a, comma", "info"}, {"two\nlines", "error"}, {`with "quotes"`, "warn"}}
	if !reflect.DeepEqual(recs, want) {
		t.Errorf("round trip = %q, want %q", recs, want)
	}
}

func TestSpec_MatchesTheDesign(t *testing.T) {
	decode := func(t *testing.T, s wireSpec) map[string]any {
		t.Helper()
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	forecast := decode(t, managed[0].spec("forsight"))
	wantForecast := map[string]any{
		"name": "forsight-cpu-forecast", "dataset": "forsight-host-cpu",
		"plugin": "holtwinters", "task": "forecast", "target": "value", "timestamp": "at",
		"metric": "rmse", "params": map[string]any{}, "tags": []any{"forsight"},
		"retrain": map[string]any{"schedule_minutes": 30.0, "cooldown_minutes": 10.0},
	}
	if !reflect.DeepEqual(forecast, wantForecast) {
		t.Errorf("forecast spec =\n%v\nwant\n%v", forecast, wantForecast)
	}
	classifier := decode(t, managed[3].spec("forsight"))
	wantClassifier := map[string]any{
		"name": "forsight-log-severity", "dataset": "forsight-logs",
		"plugin": "bayes", "task": "classification", "target": "severity",
		"features": []any{"message"}, "metric": "accuracy", "params": map[string]any{},
		"tags":    []any{"forsight"},
		"retrain": map[string]any{"min_new_labels": 50.0, "cooldown_minutes": 10.0},
	}
	if !reflect.DeepEqual(classifier, wantClassifier) {
		t.Errorf("classifier spec =\n%v\nwant\n%v", classifier, wantClassifier)
	}
}

func TestNames(t *testing.T) {
	want := []string{"agent2-cpu-forecast", "agent2-memory-forecast", "agent2-disk-forecast", "agent2-log-severity"}
	if got := Names("agent2"); !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
	if got := Names(""); got[0] != "forsight-cpu-forecast" {
		t.Errorf("Names(\"\") = %v, want the default prefix", got)
	}
	if _, ok := lookup("forsight", "forsight-cpu-forecast"); !ok {
		t.Error("lookup missed a managed name")
	}
	if _, ok := lookup("forsight", "other-cpu-forecast"); ok {
		t.Error("lookup accepted another prefix's model")
	}
}
