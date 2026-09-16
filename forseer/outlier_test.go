package forseer

import (
	"bytes"
	"math"
	"testing"
	"time"
)

// hostBatch builds one host-collector-shaped batch: the three percentages
// plus the two still-cumulative net counters, exactly what
// observeHostVectorLocked requires before it will touch the model at all.
func hostBatch(cpu, mem, disk, sent, recv float64) []Point {
	return []Point{
		{Name: "host.cpu.percent", Value: cpu},
		{Name: "host.memory.percent", Value: mem},
		{Name: "host.disk.percent", Value: disk},
		{Name: "host.net.bytes_sent", Value: sent},
		{Name: "host.net.bytes_recv", Value: recv},
	}
}

func TestDetector_HostOutlierIgnoresAPartialBatch(t *testing.T) {
	d := NewDetector()
	// A Docker or OTLP batch never carries all five host inputs together;
	// observeHostVectorLocked must not touch the model at all, let alone
	// panic on the missing values.
	for i := 0; i < hostOutlierMinSamples+5; i++ {
		d.Observe([]Point{{Name: "host.cpu.percent", Value: 50}})
	}
	if card := d.outlier.Card(); card.Ready || card.Trained != 0 {
		t.Fatalf("partial batches trained the outlier model: %+v", card)
	}
}

func TestDetector_HostOutlierNotReadyBeforeFiftySamples(t *testing.T) {
	d := NewDetector()
	sent, recv := 1_000_000.0, 800_000.0
	for i := 0; i < hostOutlierMinSamples-1; i++ {
		sent += 1000
		recv += 800
		d.Observe(hostBatch(50, 50, 30, sent, recv))
	}
	if card := d.outlier.Card(); card.Ready {
		t.Fatalf("ready after %d samples, want not ready until %d", hostOutlierMinSamples-1, hostOutlierMinSamples)
	}
}

func TestDetector_HostOutlierFlagsAJointAnomalyPerSeriesChecksMiss(t *testing.T) {
	d := NewDetector()
	fixed := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return fixed }

	sent, recv := 1_000_000.0, 800_000.0
	// cpu and memory move together almost perfectly for a long warm-up —
	// the way they do during ordinary load — while disk and the two net
	// deltas each wobble on their own independent cycle so the covariance
	// stays invertible in every dimension.
	for i := 0; i < 200; i++ {
		cpu := 50 + float64(i%5)
		jitter := []float64{0, 0.01, -0.01, 0.02, -0.02}[i%5]
		mem := cpu + jitter
		disk := 30 + float64(i%3)*0.5
		sentDelta := 1000 + float64(i%4)*20
		recvDelta := 800 + float64((i+1)%4)*15
		sent += sentDelta
		recv += recvDelta
		d.Observe(hostBatch(cpu, mem, disk, sent, recv))
	}
	if card := d.outlier.Card(); !card.Ready {
		t.Fatalf("outlier model not ready after warm-up: %+v", card)
	}
	for _, ins := range d.Insights() {
		if ins.Kind == KindHostOutlier {
			t.Fatalf("host_outlier open during the correlated warm-up: %+v", ins)
		}
	}

	// Now break the cpu/memory correlation without moving either one
	// outside its own marginal range: cpu at the top of its usual 50-54
	// cycle, memory at the bottom of its own — individually unremarkable
	// (well under the Detector's own 3σ), but a combination the warm-up
	// never showed it.
	sent += 1000
	recv += 800
	d.Observe(hostBatch(54, 50, 30.5, sent, recv))

	var found Insight
	ok := false
	for _, ins := range d.Insights() {
		if ins.Kind == KindHostOutlier {
			found, ok = ins, true
		}
		if ins.Kind == KindAnomaly {
			t.Errorf("a per-series anomaly also fired on the same batch: %+v", ins)
		}
	}
	if !ok {
		t.Fatal("expected a host_outlier insight when the cpu/memory correlation broke")
	}
	if found.Severity != SeverityWarning && found.Severity != SeverityCritical {
		t.Errorf("severity = %q, want warning or critical", found.Severity)
	}
	if found.Source != "forseer" {
		t.Errorf("source = %q, want forseer", found.Source)
	}
	if len(found.Related) != 1 || (found.Related[0] != "host.cpu.percent" && found.Related[0] != "host.memory.percent") {
		t.Errorf("related = %v, want the cpu or memory input named as the largest contributor", found.Related)
	}
	if found.Metric != found.Related[0] {
		t.Errorf("metric = %q, related = %v, want them to agree", found.Metric, found.Related)
	}

	card := d.outlier.Card()
	if card.Trained == 0 {
		t.Fatal("Card.Trained is 0 after 201 samples")
	}
	if card.Accuracy != Unmeasured || card.FallbackAccuracy != Unmeasured {
		t.Errorf("Accuracy/FallbackAccuracy = %v/%v, want Unmeasured: this job has no label",
			card.Accuracy, card.FallbackAccuracy)
	}
}

func TestDetector_HostOutlierClosesWhenTheVectorReturnsToNormal(t *testing.T) {
	d := NewDetector()
	sent, recv := 0.0, 0.0
	for i := 0; i < 200; i++ {
		cpu := 50 + float64(i%5)
		jitter := []float64{0, 0.01, -0.01, 0.02, -0.02}[i%5]
		mem := cpu + jitter
		disk := 30 + float64(i%3)*0.5
		sent += 1000 + float64(i%4)*20
		recv += 800 + float64((i+1)%4)*15
		d.Observe(hostBatch(cpu, mem, disk, sent, recv))
	}
	sent += 1000
	recv += 800
	d.Observe(hostBatch(54, 50, 30.5, sent, recv))
	opened := false
	for _, ins := range d.Insights() {
		if ins.Kind == KindHostOutlier {
			opened = true
		}
	}
	if !opened {
		t.Fatal("setup did not open a host_outlier insight")
	}

	// A single in-line batch afterwards should close it again, the same
	// way a per-series anomaly clears on a return to baseline.
	sent += 1000 + 20
	recv += 800 + 15
	d.Observe(hostBatch(51, 51.01, 30.5, sent, recv))
	for _, ins := range d.Insights() {
		if ins.Kind == KindHostOutlier {
			t.Fatalf("host_outlier still open after the vector returned to normal: %+v", ins)
		}
	}
}

func TestDetector_HostOutlierOpenedAloneCountsAgainstTheExitCriterion(t *testing.T) {
	d := NewDetector()
	sent, recv := 0.0, 0.0
	for i := 0; i < 200; i++ {
		cpu := 50 + float64(i%5)
		jitter := []float64{0, 0.01, -0.01, 0.02, -0.02}[i%5]
		mem := cpu + jitter
		disk := 30 + float64(i%3)*0.5
		sent += 1000 + float64(i%4)*20
		recv += 800 + float64((i+1)%4)*15
		d.Observe(hostBatch(cpu, mem, disk, sent, recv))
	}
	sent += 1000
	recv += 800
	d.Observe(hostBatch(54, 50, 30.5, sent, recv))

	foundHostOutlier := false
	for _, ins := range d.Insights() {
		if ins.Kind == KindHostOutlier {
			foundHostOutlier = true
		}
		if ins.Kind == KindAnomaly {
			t.Fatalf("per-series anomaly open alongside the setup meant to test the alone case: %+v", ins)
		}
	}
	if !foundHostOutlier {
		t.Fatal("expected host_outlier open for this test's premise to hold")
	}

	card := d.outlier.Card()
	if card.Detail == "no host vector has enough history yet" {
		t.Fatalf("Detail did not update after an opening: %+v", card)
	}
}

func TestDetector_HostOutlierSkipsACounterReset(t *testing.T) {
	d := NewDetector()
	d.Observe(hostBatch(50, 50, 30, 1_000_000, 800_000))
	// The net counters reset to near zero (a NIC reset, or the collector's
	// own process restarting) — the delta would be deeply negative; the
	// model must skip this vector rather than fold in a bogus reading, and
	// must not panic doing so.
	d.Observe(hostBatch(50, 50, 30, 100, 80))
	if d.outlier.Card().Trained != 0 {
		t.Fatalf("a counter reset was folded into the covariance: %+v", d.outlier.Card())
	}
	// The next batch deltas cleanly against the post-reset baseline.
	d.Observe(hostBatch(50, 50, 30, 1100, 880))
	if d.outlier.Card().Trained != 1 {
		t.Fatalf("post-reset batch was not scored: %+v", d.outlier.Card())
	}
}

func TestHostOutlierModel_Card(t *testing.T) {
	m := newHostOutlierModel()
	card := m.Card()
	if card.Name != "host outlier" {
		t.Errorf("Name = %q", card.Name)
	}
	if len(card.Reads) != hostOutlierDims {
		t.Errorf("Reads has %d entries, want %d", len(card.Reads), hostOutlierDims)
	}
	if card.Fallback == "" {
		t.Error("Fallback is empty; the contract requires naming one")
	}
	if card.Ready {
		t.Error("a fresh model reports Ready")
	}
	if card.Accuracy != Unmeasured || card.FallbackAccuracy != Unmeasured {
		t.Errorf("Accuracy/FallbackAccuracy = %v/%v, want Unmeasured", card.Accuracy, card.FallbackAccuracy)
	}
}

func TestInvert5_RoundTripsOnAWellConditionedMatrix(t *testing.T) {
	var a [hostOutlierDims][hostOutlierDims]float64
	for i := range a {
		a[i][i] = float64(i) + 2
		for j := range a[i] {
			if i != j {
				a[i][j] = 0.1
			}
		}
	}
	inv, ok := invert5(a)
	if !ok {
		t.Fatal("invert5 reported singular on a well-conditioned matrix")
	}
	// a * inv should be (approximately) the identity.
	for i := 0; i < hostOutlierDims; i++ {
		for j := 0; j < hostOutlierDims; j++ {
			var got float64
			for k := 0; k < hostOutlierDims; k++ {
				got += a[i][k] * inv[k][j]
			}
			want := 0.0
			if i == j {
				want = 1
			}
			if math.Abs(got-want) > 1e-6 {
				t.Errorf("(a * inv)[%d][%d] = %v, want %v", i, j, got, want)
			}
		}
	}
}

func TestInvert5_SingularMatrixIsRejected(t *testing.T) {
	var a [hostOutlierDims][hostOutlierDims]float64 // all zero: singular
	if _, ok := invert5(a); ok {
		t.Fatal("invert5 accepted the zero matrix")
	}
}

// warmHostOutlier feeds the same correlated warm-up pattern the Detector
// tests above use, but straight into the model, and returns the running net
// counters so a caller can keep feeding it afterwards.
func warmHostOutlier(m *hostOutlierModel, n int) (sent, recv float64) {
	sent, recv = 1_000_000.0, 800_000.0
	for i := 0; i < n; i++ {
		cpu := 50 + float64(i%5)
		jitter := []float64{0, 0.01, -0.01, 0.02, -0.02}[i%5]
		mem := cpu + jitter
		disk := 30 + float64(i%3)*0.5
		sent += 1000 + float64(i%4)*20
		recv += 800 + float64((i+1)%4)*15
		m.Observe(cpu, mem, disk, sent, recv)
	}
	return sent, recv
}

// TestHostOutlierModel_SnapshotRestoreRoundTrip is roadmap item 25's proof
// for this model: the mean vector, covariance and net-counter baseline all
// come back, so a restored model scores the very next batch exactly as a
// warm one would.
func TestHostOutlierModel_SnapshotRestoreRoundTrip(t *testing.T) {
	m := newHostOutlierModel()
	sent, recv := warmHostOutlier(m, 200)
	if !m.Card().Ready {
		t.Fatal("test setup: model not ready after warm-up")
	}

	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := newHostOutlierModel()
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !restored.Card().Ready {
		t.Fatal("restored model is not ready even though n/mean/m2 all came back")
	}
	if restored.n != m.n || restored.mean != m.mean || restored.m2 != m.m2 {
		t.Fatalf("restored model's fitted state differs from the original")
	}

	// The same next batch, fed to both, must score identically — proof the
	// restored covariance (and the net-counter baseline it deltas against)
	// is not just present but usable.
	sent += 1000
	recv += 800
	wantD2, wantMetric, wantReady := m.Observe(54, 50, 30.5, sent, recv)
	gotD2, gotMetric, gotReady := restored.Observe(54, 50, 30.5, sent, recv)
	if gotD2 != wantD2 || gotMetric != wantMetric || gotReady != wantReady {
		t.Fatalf("restored Observe = (%v,%q,%v), want (%v,%q,%v)", gotD2, gotMetric, gotReady, wantD2, wantMetric, wantReady)
	}
}

func TestHostOutlierModel_RestoreDiscardsAVersionMismatch(t *testing.T) {
	m := newHostOutlierModel()
	warmHostOutlier(m, 60)
	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1)

	fresh := newHostOutlierModel()
	if err := fresh.Restore(data); err == nil {
		t.Fatal("Restore accepted a payload with the wrong schema version")
	}
	if fresh.n != 0 {
		t.Fatalf("a discarded restore left n=%d, want 0", fresh.n)
	}
}

func TestHostOutlierModel_RestoreDiscardsCorruptJSON(t *testing.T) {
	m := newHostOutlierModel()
	if err := m.Restore([]byte("{not json")); err == nil {
		t.Fatal("Restore accepted corrupt JSON")
	}
	if m.n != 0 {
		t.Fatalf("a discarded restore left n=%d, want 0", m.n)
	}
}
