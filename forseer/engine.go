package forseer

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Engine is the Forseer surface the agent talks to. It composes the
// statistical detectors (metrics, logs, traces) so a single `forsight run`
// produces insights without a sidecar process.
type Engine struct {
	det      *Detector
	logs     *logMiner
	spans    *spanWatch
	severity *severityModel
	forecast *burnForecast
	paging   *pagingModel
	culprit  *culpritModel

	mu        sync.Mutex
	processes map[string]procSnap
	culprits  map[string]Insight
	now       func() time.Time
	errorSLO  float64
}

type procSnap struct {
	name     string
	cpu      float64
	rss      float64
	lastSeen time.Time

	// cpuZ/rssZ are this process's latest deviation from its own rolling
	// baseline (Detector.SeriesBaseline), in standard deviations, signed —
	// negative means below its own normal. The *Ready pair is false until
	// that series has minSamples of history, in which case the ranker falls
	// back to the raw floor instead of trusting a value that isn't there.
	cpuZ     float64
	cpuReady bool
	rssZ     float64
	rssReady bool
}

// NewEngine builds a live engine. Statistical detection is always on;
// Grok summary is a separate call that needs XAI_API_KEY.
func NewEngine() *Engine {
	now := time.Now
	e := &Engine{
		det:       NewDetector(),
		logs:      newLogMiner(),
		spans:     newSpanWatch(),
		severity:  newSeverityModel(),
		forecast:  newBurnForecast(),
		paging:    newPagingModel(),
		culprit:   newCulpritModel(),
		processes: make(map[string]procSnap),
		culprits:  make(map[string]Insight),
		now:       now,
		errorSLO:  defaultErrorSLO,
	}
	e.det.now = func() time.Time { return e.now() }
	e.logs.now = func() time.Time { return e.now() }
	e.spans.now = func() time.Time { return e.now() }
	e.paging.now = func() time.Time { return e.now() }
	e.logs.paging = e.paging
	return e
}

// notePaging hands the paging model every critical the other detectors
// currently hold open, so it can label the bursts they followed. Cheap: the
// open sets are bounded by insightTTL, and it runs once per observe batch,
// not per record.
func (e *Engine) notePaging() {
	insights := e.det.Insights()
	insights = append(insights, e.spans.Insights()...)
	e.mu.Lock()
	for _, ins := range e.culprits {
		insights = append(insights, ins)
	}
	e.mu.Unlock()
	e.paging.NoteInsights(insights)
}

// ObserveMetrics feeds host/process/Docker/OTLP/scrape/StatsD points.
func (e *Engine) ObserveMetrics(points []Point) {
	e.det.Observe(points)
	e.trackProcesses(points)
	e.notePaging()
}

// ObserveLogs feeds OTLP and file-tail log lines into the miner, and trains
// the severity model on the ones that arrived with a level the source
// actually declared.
func (e *Engine) ObserveLogs(lines []LogLine) {
	e.logs.Observe(lines)
	for _, line := range lines {
		if line.SeverityInferred {
			continue
		}
		e.severity.Learn(line.Message, line.Severity)
	}
}

// WithSeverityFallback tells the severity model which rule it is replacing,
// so both can be scored on the same stream and the model is used only while
// it is actually ahead. Without it the model still works and still gates on
// sample count; it just cannot report the comparison.
func (e *Engine) WithSeverityFallback(fallback func(message string) string) *Engine {
	e.severity.withFallback(fallback)
	return e
}

// ClassifySeverity answers for a log line that arrived without a level.
// The bool is false whenever the caller should keep its own fallback: the
// model has not seen enough of this deployment yet, or it is not confident
// enough about this particular line to be worth preferring over a rule the
// operator can predict.
func (e *Engine) ClassifySeverity(message string) (string, bool) {
	severity, _, ok := e.severity.Classify(message)
	return severity, ok
}

// Models reports every trained model in the engine: the job it does, the
// inputs it reads, whether it is ready, and how it is scoring. This is the
// only place the agent claims anything about what it has learned.
//
// The list itself lives in persistedModels (persist.go), which also names
// each model's persistence key — one list to keep in sync, not two, when a
// model is added.
func (e *Engine) Models() []Card {
	pms := e.persistedModels()
	cards := make([]Card, 0, len(pms))
	for _, pm := range pms {
		cards = append(cards, pm.model.Card())
	}
	return cards
}

// ObserveSpans feeds OTLP spans into the slow-span watcher.
func (e *Engine) ObserveSpans(spans []SpanSample) {
	e.spans.Observe(spans)
	e.notePaging()
}

func (e *Engine) trackProcesses(points []Point) {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, ins := range e.culprits {
		if now.Sub(ins.Time) > insightTTL {
			delete(e.culprits, k)
		}
	}
	// processes is keyed by whatever "pid" label arrives on a metric point —
	// for OTLP-sourced points that is an attacker-controlled resource
	// attribute copied verbatim by the receiver, with no cardinality
	// filtering upstream. Unlike culprits (open insights), nothing evicted
	// entries here, so this map grew without bound. Sweep it the same way,
	// on the same tick, using each pid's last-observed time rather than an
	// insight's Time: a busy but legitimate process should not be evicted
	// just because it never triggers a culprit ranking.
	for pid, snap := range e.processes {
		if now.Sub(snap.lastSeen) > insightTTL {
			delete(e.processes, pid)
		}
	}
	for _, p := range points {
		pid := ""
		if p.Labels != nil {
			pid = p.Labels["pid"]
		}
		if pid == "" {
			continue
		}
		snap := e.processes[pid]
		if p.Labels["name"] != "" {
			snap.name = p.Labels["name"]
		}
		switch p.Name {
		case "process.cpu.percent":
			snap.cpu = p.Value
			snap.cpuZ, snap.cpuReady = 0, false
			if mean, stddev, ready := e.det.SeriesBaseline(p.Name, p.Labels); ready {
				snap.cpuZ, snap.cpuReady = (p.Value-mean)/stddev, true
			}
		case "process.memory.rss_bytes":
			snap.rss = p.Value
			snap.rssZ, snap.rssReady = 0, false
			if mean, stddev, ready := e.det.SeriesBaseline(p.Name, p.Labels); ready {
				snap.rssZ, snap.rssReady = (p.Value-mean)/stddev, true
			}
		}
		snap.lastSeen = now
		e.processes[pid] = snap
	}

	hostHot := false
	for _, ins := range e.det.Insights() {
		if ins.Metric == "host.cpu.percent" && ins.Kind == KindAnomaly {
			hostHot = true
			break
		}
	}
	if !hostHot {
		return
	}
	type ranked struct {
		pid    string
		snap   procSnap
		score  float64
		metric string
		raw    float64
	}
	// changed holds processes whose own baseline says they moved; floored
	// holds ones too new to have a baseline, judged by the raw floor
	// instead. changed always outranks floored — a real baseline is better
	// evidence than the rule it replaces, never merely tied with it.
	var changed, floored []ranked
	for pid, snap := range e.processes {
		score, changeBased, metric, raw, ok := e.culprit.rank(snap)
		if !ok {
			continue
		}
		r := ranked{pid, snap, score, metric, raw}
		if changeBased {
			changed = append(changed, r)
		} else {
			floored = append(floored, r)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].score > changed[j].score })
	sort.Slice(floored, func(i, j int) bool { return floored[i].score > floored[j].score })
	top := append(changed, floored...)
	if len(top) > 3 {
		top = top[:3]
	}
	for _, r := range top {
		title := fmt.Sprintf("%s is using %.0f%% CPU while the host is anomalous", r.snap.name, r.snap.cpu)
		desc := "process ranked as a likely contributor to the host CPU spike"
		if r.metric == "process.memory.rss_bytes" {
			title = fmt.Sprintf("%s's memory rose %.1fσ above its own baseline while the host is anomalous", r.snap.name, r.score)
			desc = "process's own rss moved off its baseline; ranked as a likely contributor to the host CPU spike"
		} else if r.metric == "process.cpu.percent" && r.snap.cpuReady {
			title = fmt.Sprintf("%s's cpu rose %.1fσ above its own baseline while the host is anomalous", r.snap.name, r.score)
			desc = "process's own cpu moved off its baseline; ranked as a likely contributor to the host CPU spike"
		}
		related := []string{r.snap.name, "pid=" + r.pid}
		e.culprits[r.pid] = Insight{
			ID:          "culprit:" + r.pid,
			Kind:        KindCulprit,
			Severity:    SeverityWarning,
			Title:       title,
			Description: desc,
			Source:      r.snap.name,
			Metric:      r.metric,
			Value:       r.raw,
			Time:        now,
			Related:     related,
		}
	}
}

// Insights merges every detector, critical first.
func (e *Engine) Insights() []Insight {
	out := e.det.Insights()
	out = append(out, e.logs.Insights()...)
	out = append(out, e.spans.Insights()...)
	e.mu.Lock()
	now := e.now()
	for k, ins := range e.culprits {
		if now.Sub(ins.Time) > insightTTL {
			delete(e.culprits, k)
			continue
		}
		out = append(out, ins)
	}
	e.mu.Unlock()
	sortInsights(out)
	return out
}

// Clusters returns Drain-style log templates, busiest first.
func (e *Engine) Clusters() []Cluster {
	return e.logs.Clusters()
}

const defaultErrorSLO = 0.01 // 1% error logs

// SetErrorSLO overrides the error-log SLO Budget burns against (default 1%,
// see defaultErrorSLO — e.g. `forsight run --error-slo 0.02` for 2%). A
// non-positive value is ignored, leaving the current SLO in effect.
func (e *Engine) SetErrorSLO(slo float64) {
	if slo <= 0 {
		return
	}
	e.mu.Lock()
	e.errorSLO = slo
	e.mu.Unlock()
}

// Budget is the error-log burn against the configured SLO (1% by default),
// for ErrorBudget. The SLO itself is always reported on Budget.SLO so a
// caller — the dashboard included — never has to parse it back out of
// Caption, which is absent before any logs have landed.
func (e *Engine) Budget() Budget {
	e.mu.Lock()
	slo := e.errorSLO
	e.mu.Unlock()

	errors, total := e.logs.counts()
	consumed := 0.0
	if total > 0 && slo > 0 {
		rate := float64(errors) / float64(total)
		consumed = rate / slo * 100
		if consumed > 100 {
			consumed = 100
		}
	}
	caption := "no logs yet"
	if total > 0 {
		caption = fmt.Sprintf("%d errors in %d lines · SLO %.0f%% error logs", errors, total, slo*100)
	}

	// Reading the budget is also what feeds the forecast: the dashboard
	// polls this, so the cadence of the projection is the cadence somebody
	// is actually looking at it.
	if total > 0 {
		e.forecast.Observe(consumed, e.now())
	}
	forecastText := ""
	if soonest, latest, ok := e.forecast.Exhausted(); ok {
		if phrase := FormatProjection(soonest, latest); phrase == "already spent" {
			forecastText = "budget already spent"
		} else {
			forecastText = "exhausted " + phrase
		}
		caption += " · " + forecastText
	}

	return Budget{
		Label:     "Error-log budget",
		Consumed:  consumed,
		Caption:   caption,
		Forecast:  forecastText,
		Errors:    errors,
		Total:     total,
		SLO:       slo,
		WarningAt: 70,
		DangerAt:  90,
	}
}

// Story stitches insights into Timeline events, critical-path related names included.
func (e *Engine) Story() []Event {
	insights := e.Insights()
	out := make([]Event, 0, len(insights))
	for _, ins := range insights {
		tone := "accent"
		switch ins.Severity {
		case SeverityCritical:
			tone = "danger"
		case SeverityWarning:
			tone = "warning"
		}
		desc := ins.Kind
		if len(ins.Related) > 0 {
			desc = ins.Kind + " · " + strings.Join(ins.Related, " → ")
		}
		out = append(out, Event{
			ID:          "evt-" + ins.ID,
			Time:        ins.Time,
			Title:       ins.Title,
			Description: desc,
			Tone:        tone,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}
