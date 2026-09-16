package forseer

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

// hostOutlier answers a question none of the per-series anomaly checks can:
// is the *combination* of host.cpu.percent, host.memory.percent,
// host.disk.percent and the per-interval deltas of host.net.bytes_sent and
// host.net.bytes_recv unusual, even when no single one of the five is far
// enough from its own baseline to open on its own — a disk that is merely
// full is not the same story as a disk that filled while CPU, memory and
// network all moved together the way they do during a backup, and a
// per-series check cannot tell the two apart because it never looks at more
// than one series at a time.
//
// Method: an online mean vector and 5x5 covariance matrix (Welford's
// multivariate form — the same running update the Detector's per-series
// mean/variance uses, generalised from a scalar to a matrix), scored by
// squared Mahalanobis distance. One mean and one covariance for the whole
// model: `forsight run` is one binary per host (host.go's package comment —
// a DaemonSet mounts one node's /proc/sys into one pod), so every
// host.cpu/memory/disk/net point a single Engine ever observes already
// comes from that one host. A future collector that labelled host points by
// origin would need this keyed per label; nothing here assumes that never
// happens, it just isn't true of this binary today.
//
// The net counters are cumulative, so the two network inputs are each
// batch's delta against the previous one, not the raw counter — an
// ever-increasing count has no stationary distribution for a covariance to
// describe.
const (
	// hostOutlierDims is the number of declared inputs: cpu%, memory%,
	// disk%, and the per-interval delta of each net counter.
	hostOutlierDims = 5

	// hostOutlierMinSamples gates readiness stricter than minSamples (12):
	// a 5x5 covariance has 15 free entries to estimate, not one variance,
	// and needs more history before it is safe to invert.
	hostOutlierMinSamples = 50

	// hostOutlierWarnChi2 / hostOutlierCriticalChi2 are squared-Mahalanobis
	// thresholds: the chi-squared quantiles, at 5 degrees of freedom (one
	// per input), matching the same two-tailed-normal tail probability the
	// Detector's own warningSigma (3σ) and criticalSigma (5σ) use — 0.270%
	// and 5.73e-5%, respectively — so the host vector as a whole is called
	// out only as rarely as a single series is. Computed with
	// scipy.stats.chi2.isf(2*(1-norm.cdf(s)), df=5) (18.2053, 37.0948),
	// rounded to four significant figures.
	hostOutlierWarnChi2     = 18.21
	hostOutlierCriticalChi2 = 37.09

	// hostOutlierSingularFloor is the smallest pivot invert5 accepts before
	// calling a covariance singular. Below hostOutlierMinSamples samples,
	// or with a run of identical values in one input, the matrix can be
	// exactly or near-singular; refusing to invert it is the same "not
	// ready" answer a zero-variance per-series baseline gives.
	hostOutlierSingularFloor = 1e-12
)

// hostOutlierMetrics names the five inputs in vector order — Point.Name for
// the three percentages, and what each net counter's delta is compared
// against on an open insight (the raw counter is still what the Detector
// itself runs its own per-series check on, so this is what
// anyPerSeriesOpenLocked matches against, and what Related names as the
// largest contributor).
var hostOutlierMetrics = [hostOutlierDims]string{
	"host.cpu.percent",
	"host.memory.percent",
	"host.disk.percent",
	"host.net.bytes_sent",
	"host.net.bytes_recv",
}

// hostOutlierModel implements Model. It has no weights beyond the mean
// vector and covariance matrix themselves: Observe is a closed-form update,
// and the two counters below exist only so Card can report the exit
// criterion honestly.
type hostOutlierModel struct {
	mu   sync.Mutex
	n    int
	mean [hostOutlierDims]float64
	m2   [hostOutlierDims][hostOutlierDims]float64
	// ready mirrors the last Observe call's covariance-invertibility check,
	// so Card doesn't have to re-invert the matrix just to answer Ready.
	ready bool

	prevSent, prevRecv float64
	havePrevNet        bool

	// opened counts every transition from no host_outlier insight open to
	// one open; openedAlone is the subset of those where, at the moment it
	// opened, no per-series anomaly (Detector.KindAnomaly) was open on any
	// of the five inputs. That subset is the exit criterion the roadmap
	// item names: if it stays zero over a watched period, this model is
	// finding nothing the per-series checks were not already finding, and
	// should be retired.
	opened      int
	openedAlone int
}

func newHostOutlierModel() *hostOutlierModel {
	return &hostOutlierModel{}
}

// reading turns this batch's three percentages and two still-cumulative net
// counters into the joint vector to score, and folds it into the running
// mean/covariance. ok is false for the batch that establishes the first net
// baseline (nothing to delta against yet) and for a counter reset — either
// delta going negative, from a NIC reset or the collector's own process
// restarting — in both cases the new counters are still recorded so the
// next batch gets a valid delta; the mean/covariance are not updated on
// either, since there is no valid vector to fold in.
func (m *hostOutlierModel) reading(cpu, mem, disk, sent, recv float64) (vec [hostOutlierDims]float64, ok bool) {
	prevSent, prevRecv, havePrev := m.prevSent, m.prevRecv, m.havePrevNet
	m.prevSent, m.prevRecv, m.havePrevNet = sent, recv, true
	if !havePrev {
		return vec, false
	}
	dSent, dRecv := sent-prevSent, recv-prevRecv
	if dSent < 0 || dRecv < 0 {
		return vec, false
	}
	return [hostOutlierDims]float64{cpu, mem, disk, dSent, dRecv}, true
}

// Observe folds one batch's readings into the online mean/covariance and
// reports the squared Mahalanobis distance of that same reading against the
// just-updated statistics, plus which of the five inputs contributed most
// to it. ready is false until hostOutlierMinSamples joint samples have
// accumulated and the resulting covariance is invertible — the same shape
// of answer Detector.SeriesBaseline gives for a cold or zero-variance
// series, generalised to a matrix. metric is the empty string when ready is
// false.
func (m *hostOutlierModel) Observe(cpu, mem, disk, sent, recv float64) (d2 float64, metric string, ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	vec, ok := m.reading(cpu, mem, disk, sent, recv)
	if !ok {
		return 0, "", false
	}

	m.n++
	n := float64(m.n)
	var delta, delta2 [hostOutlierDims]float64
	for i := 0; i < hostOutlierDims; i++ {
		delta[i] = vec[i] - m.mean[i]
		m.mean[i] += delta[i] / n
		delta2[i] = vec[i] - m.mean[i]
	}
	for i := 0; i < hostOutlierDims; i++ {
		for j := 0; j < hostOutlierDims; j++ {
			m.m2[i][j] += delta[i] * delta2[j]
		}
	}

	if m.n < hostOutlierMinSamples {
		m.ready = false
		return 0, "", false
	}

	var cov [hostOutlierDims][hostOutlierDims]float64
	divisor := n - 1
	for i := range cov {
		for j := range cov[i] {
			cov[i][j] = m.m2[i][j] / divisor
		}
	}
	inv, ok := invert5(cov)
	if !ok {
		m.ready = false
		return 0, "", false
	}
	m.ready = true

	var current [hostOutlierDims]float64
	for i := 0; i < hostOutlierDims; i++ {
		current[i] = vec[i] - m.mean[i]
	}
	d2, contrib := mahalanobis(current, inv)
	top := 0
	for i := 1; i < hostOutlierDims; i++ {
		if math.Abs(contrib[i]) > math.Abs(contrib[top]) {
			top = i
		}
	}
	return d2, hostOutlierMetrics[top], true
}

// NoteOpened records one host_outlier insight opening (a transition from
// closed to open, never a re-scoring of one already open) and whether, at
// that moment, no per-series anomaly was open on any of the five inputs —
// see the field comments above for why that subset is the exit criterion.
func (m *hostOutlierModel) NoteOpened(alone bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opened++
	if alone {
		m.openedAlone++
	}
}

// Card implements Model.
func (m *hostOutlierModel) Card() Card {
	m.mu.Lock()
	defer m.mu.Unlock()

	detail := "no host vector has enough history yet"
	if m.n > 0 {
		detail = fmt.Sprintf(
			"opened %d time(s), %d of them with no per-series anomaly open on any of the five inputs — the exit criterion: if that count stays zero over a watched period, retire this model",
			m.opened, m.openedAlone)
	}

	return Card{
		Name: "host outlier",
		Job:  "Say whether the host's cpu, memory, disk and network look unusual together, even when no single one does on its own.",
		Reads: []string{
			"host.cpu.percent",
			"host.memory.percent",
			"host.disk.percent",
			"host.net.bytes_sent, per-interval delta",
			"host.net.bytes_recv, per-interval delta",
		},
		Fallback: "the per-series z-score anomaly check the Detector already runs on each of the five inputs",
		Ready:    m.ready,
		Trained:  m.n,
		// Nobody labels "this combination of host metrics was really
		// unusual" — see the Detail line above, which is the closest thing
		// this job has to a score: how often it found something the
		// per-series checks did not.
		Accuracy:         Unmeasured,
		FallbackAccuracy: Unmeasured,
		Graded:           0,
		Detail:           detail,
	}
}

// hostOutlierSnapshotVersion is this model's own schema version — see
// severitySnapshotVersion's comment for what that guards against.
const hostOutlierSnapshotVersion = 1

// hostOutlierSnapshot is Snapshot's JSON payload: the online mean vector and
// covariance accumulator, the previous net-counter reading, and the
// exit-criterion counters — the entire fitted model, with nothing to reset.
type hostOutlierSnapshot struct {
	Version     int                                       `json:"version"`
	N           int                                       `json:"n"`
	Mean        [hostOutlierDims]float64                  `json:"mean"`
	M2          [hostOutlierDims][hostOutlierDims]float64 `json:"m2"`
	Ready       bool                                      `json:"ready"`
	PrevSent    float64                                   `json:"prevSent"`
	PrevRecv    float64                                   `json:"prevRecv"`
	HavePrevNet bool                                      `json:"havePrevNet"`
	Opened      int                                       `json:"opened"`
	OpenedAlone int                                       `json:"openedAlone"`
}

// Snapshot implements Model.
func (m *hostOutlierModel) Snapshot() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := hostOutlierSnapshot{
		Version: hostOutlierSnapshotVersion, N: m.n, Mean: m.mean, M2: m.m2, Ready: m.ready,
		PrevSent: m.prevSent, PrevRecv: m.prevRecv, HavePrevNet: m.havePrevNet,
		Opened: m.opened, OpenedAlone: m.openedAlone,
	}
	return json.Marshal(snap)
}

// Restore implements Model. The mean vector, covariance accumulator and
// sample count come back — that is the fitted Gaussian this model scores
// against — and Ready comes back as it was snapshotted rather than
// re-derived by inverting the matrix again here, since it already reflects
// the same n and m2 being restored alongside it.
//
// The previous net-counter reading (prevSent, prevRecv, havePrevNet) also
// comes back: host.net.bytes_sent/recv are counters that persist across an
// agent restart — they reset only on a NIC reset or a host reboot, neither
// of which this process restarting is — so keeping them avoids treating the
// first batch after a restart as a fresh baseline with nothing to delta
// against, the same way reading()'s own comment describes for a live NIC
// reset.
//
// opened/openedAlone — the exit-criterion counters Card reports — come back
// too, on the same reasoning as culpritModel's ranked/floored: there is no
// prequential window here to re-earn, just a running count of what this
// model has actually found.
func (m *hostOutlierModel) Restore(data []byte) error {
	var snap hostOutlierSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("host outlier snapshot: %w", err)
	}
	if snap.Version != hostOutlierSnapshotVersion {
		return fmt.Errorf("host outlier snapshot version %d, want %d", snap.Version, hostOutlierSnapshotVersion)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n, m.mean, m.m2, m.ready = snap.N, snap.Mean, snap.M2, snap.Ready
	m.prevSent, m.prevRecv, m.havePrevNet = snap.PrevSent, snap.PrevRecv, snap.HavePrevNet
	m.opened, m.openedAlone = snap.Opened, snap.OpenedAlone
	return nil
}

// invert5 returns the inverse of a 5x5 matrix by Gauss-Jordan elimination
// with partial pivoting, and false if it is singular (or too close to it to
// invert reliably) — which a covariance built from fewer than
// hostOutlierDims independent directions of variation always is.
func invert5(a [hostOutlierDims][hostOutlierDims]float64) (inv [hostOutlierDims][hostOutlierDims]float64, ok bool) {
	const dims = hostOutlierDims
	var aug [dims][2 * dims]float64
	for i := 0; i < dims; i++ {
		copy(aug[i][:dims], a[i][:])
		aug[i][dims+i] = 1
	}
	for col := 0; col < dims; col++ {
		pivot, maxAbs := col, math.Abs(aug[col][col])
		for r := col + 1; r < dims; r++ {
			if v := math.Abs(aug[r][col]); v > maxAbs {
				pivot, maxAbs = r, v
			}
		}
		if maxAbs < hostOutlierSingularFloor {
			return inv, false
		}
		aug[col], aug[pivot] = aug[pivot], aug[col]
		pv := aug[col][col]
		for k := 0; k < 2*dims; k++ {
			aug[col][k] /= pv
		}
		for r := 0; r < dims; r++ {
			if r == col {
				continue
			}
			factor := aug[r][col]
			if factor == 0 {
				continue
			}
			for k := 0; k < 2*dims; k++ {
				aug[r][k] -= factor * aug[col][k]
			}
		}
	}
	for i := 0; i < dims; i++ {
		copy(inv[i][:], aug[i][dims:])
	}
	return inv, true
}

// mahalanobis returns the squared Mahalanobis distance of delta (the
// reading minus the mean) against the inverse covariance inv, decomposed
// into each input's contribution: contrib[i] is delta[i] times the i-th
// component of inv·delta, and the contributions sum to d2. Off-diagonal
// covariance means a contribution is not a variance share in the strict
// sense, but it is exactly the term invert5's own arithmetic assigns to
// input i, which is enough to name the largest one the way culprit names
// the process that moved most.
func mahalanobis(delta [hostOutlierDims]float64, inv [hostOutlierDims][hostOutlierDims]float64) (d2 float64, contrib [hostOutlierDims]float64) {
	var iv [hostOutlierDims]float64
	for i := 0; i < hostOutlierDims; i++ {
		var s float64
		for j := 0; j < hostOutlierDims; j++ {
			s += inv[i][j] * delta[j]
		}
		iv[i] = s
	}
	for i := 0; i < hostOutlierDims; i++ {
		contrib[i] = delta[i] * iv[i]
		d2 += contrib[i]
	}
	return d2, contrib
}
