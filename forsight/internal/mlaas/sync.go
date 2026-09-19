package mlaas

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Fors-Corp/forsight/forsight/internal/logsafe"
	"github.com/Fors-Corp/forsight/forsight/internal/model"
	"github.com/Fors-Corp/forsight/forsight/internal/store"
)

const (
	defaultSyncInterval = 5 * time.Minute

	// statusTTL is how old the cached snapshot may be before Status
	// refreshes it from mlaas. The dashboard polls every few seconds; four
	// health calls per poll would make mlaas re-run its drift check that
	// often for no new information.
	statusTTL = 15 * time.Second

	// feedbackBatch is how many declared-severity lines one pass sends
	// through the classifier and labels. It is a sample, not the firehose:
	// the point is a live accuracy and a retrain trigger that mean
	// something, not a copy of the log in mlaas.
	feedbackBatch = 20
	// maxPredictions is how many of those lines the snapshot keeps for the
	// "recent predictions" table.
	maxPredictions = 50

	// maxPredictRows bounds a proxied Predict: the form on the Models page
	// sends one line; ten is generous, and anything larger belongs on
	// mlaas's own API, not on the agent's dashboard route.
	maxPredictRows = 10

	// jobsLimit is how many recent jobs the refresh asks for before
	// filtering to the managed models (mlaas caps /jobs at 1000). Twenty
	// covers a day of scheduled forecast retrains on its own, but mlaas is
	// meant to be shared — another tenant's models on the same server can
	// generate jobs too, and a 20-row page is exactly wide enough for their
	// activity to push all four of ours off the end. Two hundred is
	// headroom for that shared-server case, not the common one.
	jobsLimit = 200

	// jobsShown is how many of the managed models' jobs the snapshot keeps
	// after filtering. The dashboard's job table is a recent-activity view,
	// not an archive: on a busy day the scheduled retrains alone would
	// otherwise scroll it past the fold.
	jobsShown = 20
)

// forecastHorizons are the instants after the series' origin each
// forecast model is asked about: enough points to draw a line an hour out
// without asking Holt-Winters for sixty of them.
var forecastHorizons = []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 60 * time.Minute}

// nameRe mirrors mlaas's own rule for a dataset or model name (see
// store.validName in the mlaas source). Config.Prefix feeds every name this
// agent derives — Names, managedModel.name, managedModel.dataset — so a
// prefix that would produce an invalid one is worth failing on at New()
// with a clear reason, rather than as a 400 from the first call of the
// first pass.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// validatePrefix checks every name managed derives from prefix and reports
// the longest offender, which is usually why it failed: mlaas's rule caps
// the whole name at 64 bytes, so a prefix that is fine on its own can still
// push "<prefix>-memory-forecast" over the limit.
func validatePrefix(prefix string) error {
	var longest string
	for _, m := range managed {
		for _, name := range [...]string{m.name(prefix), m.dataset(prefix)} {
			if !nameRe.MatchString(name) && len(name) > len(longest) {
				longest = name
			}
		}
	}
	if longest == "" {
		return nil
	}
	return fmt.Errorf("mlaas: prefix %q: every derived name must match %s (longest is %q)", prefix, nameRe.String(), longest)
}

// Syncer owns the integration: the timed sync pass, the status snapshot the
// API serves, and the three proxied actions. One per agent process.
type Syncer struct {
	cfg        Config
	configured bool
	client     *Client
	store      store.Store
	classify   func(string) (string, bool)
	logger     *slog.Logger
	// displayURL is cfg.URL with any userinfo password redacted (see
	// url.URL.Redacted): what Status.URL and every log line show, so an
	// operator who puts credentials in --mlaas-url never sees them printed
	// back.
	displayURL string
	// now is the clock; tests replace it to drive the status TTL and to
	// pin bucket boundaries.
	now func() time.Time

	// refreshMu serialises refreshes so ten dashboard tabs polling at once
	// cost mlaas one round of calls, not ten.
	refreshMu sync.Mutex

	// mu guards everything below. It is never held across a network call.
	mu          sync.Mutex
	snap        Status
	refreshedAt time.Time
	// passErr and refreshErr are kept apart so a pass's error stays on the
	// page until the next pass, however many refreshes succeed in between.
	passErr, refreshErr string
	// exported records, per model name, whether the last pass had enough
	// rows to register the dataset — the "waiting-for-data" input to the
	// state derivation.
	exported map[string]bool
	// forecasts is the last projection per forecast model, by model name.
	forecasts map[string]Forecast
	// predictions is the recent-lines table, newest first.
	predictions []Prediction
	// fedThrough is the timestamp of the newest log line the feedback loop
	// has sent; fedPrimed is false until the first batch picks a start.
	fedThrough time.Time
	fedPrimed  bool
	// uploadedThrough is the newest exported bucket's "at" mlaas has been
	// given, per forecast dataset, set only after a successful
	// UploadDataset. A forecast series keeps growing by one row a minute
	// forever, but once its export hits maxForecastRows the row count stops
	// changing — the retention window just slides, one bucket ageing out
	// for every one that arrives — so the count alone would freeze re-
	// uploads the moment the window filled. Comparing the newest bucket
	// instead of the count catches that. The severity dataset has no
	// per-row timestamp to compare (its rows are declared-severity lines,
	// not a time series) and stays on the count rule alone: it is the
	// feedback loop, not re-uploads, that grows that corpus over time.
	uploadedThrough map[string]time.Time
	// mlaasVersion is the version mlaas last reported on GET /healthz, ""
	// before the first successful probe. noteVersion is what maintains it.
	mlaasVersion string
}

// New builds a Syncer. An empty cfg.URL is not an error: it means the
// integration is off, Status reports Configured=false, and Run returns at
// once. A URL that is set must parse with an http or https scheme and a
// host — the one check that catches "8090" or "localhost:8090" (which
// url.Parse reads as a scheme) before the first pass fails on every call.
// classify may be nil, in which case the recorded Forseer answer is always
// empty; logger may be nil.
func New(cfg Config, st store.Store, classify func(string) (string, bool), logger *slog.Logger) (*Syncer, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Prefix == "" {
		cfg.Prefix = DefaultPrefix
	}
	if err := validatePrefix(cfg.Prefix); err != nil {
		return nil, err
	}
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = defaultSyncInterval
	}
	s := &Syncer{
		cfg:             cfg,
		store:           st,
		classify:        classify,
		logger:          logger,
		now:             time.Now,
		exported:        map[string]bool{},
		forecasts:       map[string]Forecast{},
		uploadedThrough: map[string]time.Time{},
	}
	if cfg.URL == "" {
		return s, nil
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("mlaas: url %q: %w", cfg.URL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("mlaas: url %q must be http://host:port or https://host:port", cfg.URL)
	}
	if st == nil {
		return nil, errors.New("mlaas: a store is required to export datasets")
	}
	s.configured = true
	s.client = NewClient(cfg.URL, cfg.APIKey)
	// Redacted, not the raw cfg.URL: a URL an operator wrote with
	// credentials in it (http://user:pass@host) should never be echoed back
	// whole, not in the status the dashboard serves and not in a log line.
	s.displayURL = u.Redacted()
	s.snap = Status{Configured: true, URL: s.displayURL, Models: s.initialModels()}
	return s, nil
}

// DisplayURL is the configured URL with any userinfo password redacted
// (see url.URL.Redacted) — what every log line about this Syncer should
// show. Empty when the integration is not configured.
func (s *Syncer) DisplayURL() string { return s.displayURL }

// initialModels is the snapshot before the first contact: every managed
// model with its static description and nothing learned yet.
func (s *Syncer) initialModels() []ModelStatus {
	out := make([]ModelStatus, len(managed))
	for i, m := range managed {
		out[i] = s.staticStatus(m)
	}
	return out
}

// staticStatus is the part of a ModelStatus that comes from the table
// rather than from mlaas.
func (s *Syncer) staticStatus(m managedModel) ModelStatus {
	return ModelStatus{
		Name:    m.name(s.cfg.Prefix),
		Job:     m.job,
		Task:    m.task,
		Plugin:  m.plugin,
		Dataset: m.dataset(s.cfg.Prefix),
		Reads:   append([]string(nil), m.reads...),
		Metric:  m.metric,
		State:   StateWaitingForData,
	}
}

// Run does a pass now and then one every SyncInterval until ctx is done.
// Each call inside a pass carries its own deadline, so a stuck server
// delays the pass, never the shutdown. Not configured: returns at once.
func (s *Syncer) Run(ctx context.Context) {
	if !s.configured {
		return
	}
	s.pass(ctx)
	t := time.NewTicker(s.cfg.SyncInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.pass(ctx)
		}
	}
}

// pass is one export-and-feedback cycle: probe, list what mlaas has, hand
// each managed model to syncModel (export, upload when grown, register and
// train), then run the two feedback loops over what syncModel produced. The
// first error is kept for the page; later ones are logged. Every step that
// fails is skipped, not fatal, because the next pass retries all of it
// anyway. pass owns only the orchestration — firstErr/note, and finish,
// which writes it to the snapshot.
func (s *Syncer) pass(ctx context.Context) {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	finish := func() {
		now := s.now()
		s.mu.Lock()
		s.snap.LastSync = now
		s.passErr = ""
		if firstErr != nil {
			s.passErr = firstErr.Error()
		}
		s.snap.LastError = s.lastErrorLocked()
		s.mu.Unlock()
	}

	version, err := s.client.Healthz(ctx)
	if err != nil {
		s.logger.Warn("mlaas unreachable; skipping sync pass", "url", s.displayURL, "err", logsafe.Err(err))
		note(err)
		s.markUnreachable(err)
		finish()
		return
	}
	s.noteVersion(version)
	datasets, err := s.client.ListDatasets(ctx)
	if err != nil {
		s.logger.Warn("mlaas: list datasets", "err", logsafe.Err(err))
		note(err)
		s.markUnreachable(err)
		finish()
		return
	}
	models, err := s.client.ListModels(ctx)
	if err != nil {
		s.logger.Warn("mlaas: list models", "err", logsafe.Err(err))
		note(err)
		s.markUnreachable(err)
		finish()
		return
	}
	rowsByDataset := map[string]int{}
	for _, d := range datasets {
		rowsByDataset[d.Name] = d.Rows
	}
	byName := map[string]wireModel{}
	for _, m := range models {
		byName[m.Name] = m
	}

	// Log lines are read once per pass; the classifier's export and the
	// feedback loop both want them.
	var logs []model.LogEntry
	var logsErr error
	logsLoaded := false
	loadLogs := func() ([]model.LogEntry, error) {
		if !logsLoaded {
			logs, logsErr = s.store.QueryLogs(ctx, store.LogQuery{})
			logsLoaded = true
		}
		return logs, logsErr
	}

	exports := map[string][]row{}
	for _, m := range managed {
		s.syncModel(ctx, m, byName, rowsByDataset, exports, loadLogs, note)
	}

	for _, m := range managed {
		if m.task != taskForecast {
			continue
		}
		name := m.name(s.cfg.Prefix)
		rows, ok := exports[name]
		wm := byName[name]
		if !ok || wm.Champion == nil {
			continue
		}
		note(s.forecast(ctx, m, rows))
	}

	for _, m := range managed {
		if m.task != taskClassification {
			continue
		}
		name := m.name(s.cfg.Prefix)
		wm, ok := byName[name]
		if !ok || wm.Champion == nil {
			continue
		}
		lines, err := loadLogs()
		if err != nil {
			note(err)
			continue
		}
		note(s.feedback(ctx, m, wm.Classes, lines))
	}

	// The pass changed what mlaas holds; the page should see that on its
	// next poll rather than a snapshot from up to 15s before the pass.
	s.refresh(ctx, true)
	finish()
}

// syncModel is pass's per-model step: export the dataset, upload it when it
// grew (with a forecast-specific re-upload rule — see uploadedThrough's
// doc), register the model with mlaas if it is not in byName yet (recovering
// the real model on a concurrent-registration 409 rather than assuming a
// bare one), and queue training when there is no champion yet. byName,
// rowsByDataset and exports are the maps pass built for the whole batch;
// syncModel reads and updates them in place so later models in the same
// pass, and pass's own forecast/feedback loops afterward, see the result.
// Every failure calls note and returns, exactly like pass's own continue-on-
// error loop did before the split, because the next pass retries all of it
// anyway.
func (s *Syncer) syncModel(ctx context.Context, m managedModel, byName map[string]wireModel, rowsByDataset map[string]int, exports map[string][]row, loadLogs func() ([]model.LogEntry, error), note func(error)) {
	name, ds := m.name(s.cfg.Prefix), m.dataset(s.cfg.Prefix)
	rows, err := s.export(ctx, m, loadLogs)
	if err != nil {
		s.logger.Warn("mlaas: export", "dataset", ds, "err", err)
		note(err)
		return
	}
	enough := m.enough(rows)
	s.mu.Lock()
	s.exported[name] = enough
	s.mu.Unlock()
	if !enough {
		return
	}
	exports[name] = rows
	have, dsKnown := rowsByDataset[ds]
	needUpload := !dsKnown || have != len(rows)
	if !needUpload && m.task == taskForecast {
		// The count alone freezes here: once the export hits
		// maxForecastRows the row count stops changing forever, but the
		// window is still sliding — a bucket ages out for every one
		// that arrives. Re-upload whenever the newest exported bucket
		// is one mlaas has not seen yet (see uploadedThrough's doc).
		s.mu.Lock()
		last := s.uploadedThrough[ds]
		s.mu.Unlock()
		needUpload = !rows[len(rows)-1].at.Equal(last)
	}
	if needUpload {
		csvBytes, err := writeCSV(m.header(), rows)
		if err != nil {
			note(err)
			return
		}
		if _, err := s.client.UploadDataset(ctx, ds, csvBytes); err != nil {
			s.logger.Warn("mlaas: upload dataset", "dataset", logsafe.String(ds), "err", logsafe.Err(err))
			note(err)
			return
		}
		s.logger.Info("mlaas: uploaded dataset", "dataset", ds, "rows", len(rows))
		rowsByDataset[ds] = len(rows)
		if m.task == taskForecast {
			s.mu.Lock()
			s.uploadedThrough[ds] = rows[len(rows)-1].at
			s.mu.Unlock()
		}
	}

	wm, known := byName[name]
	var health *wireHealth
	if !known {
		created, err := s.client.CreateModel(ctx, m.spec(s.cfg.Prefix))
		switch {
		case err == nil:
			s.logger.Info("mlaas: created model", "model", name)
			wm = created
		case upstreamStatus(err) == 409:
			// Registered by an earlier run, or by another agent with
			// the same prefix; it was just not in the list we read.
			// Recover the real model rather than a bare
			// wireModel{Name: name} — in particular its Classes, which
			// the feedback loop below needs this same pass to filter a
			// declared level mlaas has never seen out of the batch
			// instead of sending it and having the whole batch refused.
			got, err := s.client.GetModel(ctx, name)
			if err != nil {
				s.logger.Warn("mlaas: get model", "model", logsafe.String(name), "err", logsafe.Err(err))
				note(err)
				return
			}
			wm = got
		default:
			s.logger.Warn("mlaas: create model", "model", logsafe.String(name), "err", logsafe.Err(err))
			note(err)
			return
		}
		// Re-read: the health answer carries the champion (there may
		// be one behind a 409) and whether a job is already running.
		h, err := s.client.GetHealth(ctx, name)
		if err != nil {
			s.logger.Warn("mlaas: model health", "model", logsafe.String(name), "err", logsafe.Err(err))
			note(err)
			return
		}
		wm.Champion = h.Champion
		health = &h
		byName[name] = wm
	}
	if wm.Champion == nil {
		if health == nil {
			h, err := s.client.GetHealth(ctx, name)
			if err != nil {
				s.logger.Warn("mlaas: model health", "model", logsafe.String(name), "err", logsafe.Err(err))
				note(err)
				return
			}
			health = &h
		}
		if !health.ActiveJob {
			ref, err := s.client.Train(ctx, name)
			if err != nil {
				s.logger.Warn("mlaas: train", "model", logsafe.String(name), "err", logsafe.Err(err))
				note(err)
				return
			}
			s.logger.Info("mlaas: queued training", "model", name, "job", ref.JobID)
		}
	}
}

// export reads the store and builds a managed model's dataset rows.
func (s *Syncer) export(ctx context.Context, m managedModel, loadLogs func() ([]model.LogEntry, error)) ([]row, error) {
	switch m.task {
	case taskForecast:
		metrics, err := s.store.QueryMetrics(ctx, store.MetricQuery{Name: m.sourceMetric})
		if err != nil {
			return nil, err
		}
		return forecastRows(metrics, s.now()), nil
	default:
		logs, err := loadLogs()
		if err != nil {
			return nil, err
		}
		return severityRows(logs), nil
	}
}

// header is the CSV header of a managed model's dataset.
func (m managedModel) header() []string {
	if m.task == taskForecast {
		return []string{forecastTimeColumn, forecastValueColumn}
	}
	return []string{severityTextColumn, severityLabelColumn}
}

// enough reports whether an export has what its model needs to train on.
func (m managedModel) enough(rows []row) bool {
	if m.task == taskForecast {
		return len(rows) >= minForecastRows
	}
	return len(rows) >= minSeverityRows && distinctLabels(rows) >= minSeverityClasses
}

// forecast is step four for one model: tell mlaas the series' newest
// instant so it scores the forecasts that instant settles, then ask for
// the next hour and cache the answer. Any predict failure drops the cached
// forecast: a stale line on the chart is worse than none.
func (s *Syncer) forecast(ctx context.Context, m managedModel, rows []row) error {
	name := m.name(s.cfg.Prefix)
	origin := rows[len(rows)-1].at
	var firstErr error
	if _, err := s.client.Actual(ctx, name, origin); err != nil {
		s.logger.Warn("mlaas: actual", "model", logsafe.String(name), "err", logsafe.Err(err))
		firstErr = err
	}
	predRows := make([]map[string]any, len(forecastHorizons))
	for i, h := range forecastHorizons {
		predRows[i] = map[string]any{forecastTimeColumn: origin.Add(h).UTC().Format(timeLayout)}
	}
	resp, err := s.client.Predict(ctx, name, predRows)
	if err == nil && len(resp.Predictions) != len(predRows) {
		err = fmt.Errorf("mlaas answered %d predictions for %d rows", len(resp.Predictions), len(predRows))
	}
	var points []ForecastPoint
	if err == nil {
		points = make([]ForecastPoint, 0, len(predRows))
		for i, p := range resp.Predictions {
			v, perr := strconv.ParseFloat(p.Value, 64)
			if perr != nil {
				err = fmt.Errorf("mlaas forecast value %q: %w", p.Value, perr)
				break
			}
			points = append(points, ForecastPoint{At: origin.Add(forecastHorizons[i]), Value: v})
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.logger.Warn("mlaas: predict", "model", logsafe.String(name), "err", logsafe.Err(err))
		delete(s.forecasts, name)
		if firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	s.forecasts[name] = Forecast{Model: name, Metric: m.sourceMetric, Origin: origin, Points: points}
	return firstErr
}

// feedback is step five: send a sample of newly arrived declared-severity
// lines through the classifier, post the declared level back as the
// outcome, and record what each model said. The cursor advances only when
// the predict call succeeds, so a failed pass retries the same lines.
//
// Which lines: the newest feedbackBatch after the cursor, not the oldest.
// A burst of a thousand lines would otherwise keep the loop a pass behind
// for the next fifty passes; a sample that stays current is the point.
func (s *Syncer) feedback(ctx context.Context, m managedModel, classes []string, logs []model.LogEntry) error {
	name := m.name(s.cfg.Prefix)
	s.mu.Lock()
	cursor, primed := s.fedThrough, s.fedPrimed
	s.mu.Unlock()

	lines := make([]model.LogEntry, 0, len(logs))
	for _, l := range logs {
		if l.SeverityInferred || l.Severity == "" || truncateMessage(l.Message) == "" {
			continue
		}
		if primed && !l.Timestamp.After(cursor) {
			continue
		}
		lines = append(lines, l)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Timestamp.Before(lines[j].Timestamp) })
	if len(lines) > feedbackBatch {
		lines = lines[len(lines)-feedbackBatch:]
	}
	if len(lines) == 0 {
		return nil
	}

	rows := make([]map[string]any, len(lines))
	for i, l := range lines {
		rows[i] = map[string]any{severityTextColumn: truncateMessage(l.Message)}
	}
	resp, err := s.client.Predict(ctx, name, rows)
	if err != nil {
		s.logger.Warn("mlaas: predict", "model", logsafe.String(name), "err", logsafe.Err(err))
		return err
	}
	if len(resp.Predictions) != len(rows) {
		err := fmt.Errorf("mlaas answered %d predictions for %d rows", len(resp.Predictions), len(rows))
		s.logger.Warn("mlaas: predict", "model", name, "err", err)
		return err
	}

	// Labels mlaas would refuse are left out rather than sent: one level
	// the model has never seen ("trace", say) would fail the whole batch,
	// and the lines that carry it still belong in the table.
	known := map[string]bool{}
	for _, c := range classes {
		known[c] = true
	}
	labels := make([]wireLabel, 0, len(lines))
	recorded := make([]Prediction, 0, len(lines))
	for i, l := range lines {
		declared := string(l.Severity)
		if len(classes) == 0 || known[declared] {
			labels = append(labels, wireLabel{PredictionID: resp.Predictions[i].PredictionID, Label: declared})
		}
		p := Prediction{At: l.Timestamp, Message: rows[i][severityTextColumn].(string), Declared: declared, Mlaas: resp.Predictions[i].Value}
		if s.classify != nil {
			if sev, ok := s.classify(p.Message); ok {
				p.Forseer = sev
			}
		}
		recorded = append(recorded, p)
	}
	var feedbackErr error
	if len(labels) > 0 {
		if _, err := s.client.Feedback(ctx, labels); err != nil {
			s.logger.Warn("mlaas: feedback", "model", logsafe.String(name), "err", logsafe.Err(err))
			feedbackErr = err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Newest first: the batch is oldest-first, so walk it backwards onto
	// the front.
	fresh := make([]Prediction, 0, len(recorded)+len(s.predictions))
	for i := len(recorded) - 1; i >= 0; i-- {
		fresh = append(fresh, recorded[i])
	}
	fresh = append(fresh, s.predictions...)
	if len(fresh) > maxPredictions {
		fresh = fresh[:maxPredictions]
	}
	s.predictions = fresh
	s.fedThrough = lines[len(lines)-1].Timestamp
	s.fedPrimed = true
	return feedbackErr
}

// noteVersion records the version mlaas reported on a successful Healthz
// and logs it: once when a version is first seen, and again whenever it
// differs from the last one recorded. A changed version between sync
// passes means mlaas was upgraded underneath a running agent — the
// interesting event; the routine case of the same version every five
// minutes stays silent. version empty (an old mlaas whose /healthz omits
// the field) is a no-op.
func (s *Syncer) noteVersion(version string) {
	if version == "" {
		return
	}
	s.mu.Lock()
	prev := s.mlaasVersion
	s.mlaasVersion = version
	s.mu.Unlock()
	if prev == version {
		return
	}
	if prev == "" {
		s.logger.Info("mlaas: version", "version", version)
		return
	}
	s.logger.Info("mlaas: version changed", "from", prev, "to", version)
}

// markUnreachable records a failed probe without touching what was learned
// before it.
func (s *Syncer) markUnreachable(err error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Reachable = false
	s.snap.CheckedAt = now
	s.refreshErr = err.Error()
	s.snap.LastError = s.lastErrorLocked()
	s.refreshedAt = now
}

// lastErrorLocked is what the page shows: the pass's error while there is
// one, else the refresh's.
func (s *Syncer) lastErrorLocked() string {
	if s.passErr != "" {
		return s.passErr
	}
	return s.refreshErr
}

// Status is the snapshot for GET /api/v1/mlaas/status, refreshed from
// mlaas when older than statusTTL. Forecasts and predictions come from the
// sync pass; everything else is re-read here. A failed refresh keeps the
// previous values and marks the server unreachable.
func (s *Syncer) Status(ctx context.Context) Status {
	if !s.configured {
		return Status{Models: []ModelStatus{}, Forecasts: []Forecast{}, Predictions: []Prediction{}, Jobs: []Job{}}
	}
	s.refresh(ctx, false)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snap
	out.Models = append([]ModelStatus(nil), s.snap.Models...)
	out.Jobs = append([]Job(nil), s.snap.Jobs...)
	if out.Jobs == nil {
		out.Jobs = []Job{}
	}
	out.Forecasts = make([]Forecast, 0, len(s.forecasts))
	for _, m := range managed {
		if f, ok := s.forecasts[m.name(s.cfg.Prefix)]; ok {
			out.Forecasts = append(out.Forecasts, f)
		}
	}
	out.Predictions = append([]Prediction{}, s.predictions...)
	return out
}

// refresh re-reads mlaas into the snapshot. With force false it is a no-op
// while the snapshot is younger than statusTTL — checked again under
// refreshMu, so callers that queued behind a refresh do not repeat it.
func (s *Syncer) refresh(ctx context.Context, force bool) {
	// ctx is usually a dashboard request's context — cancelled the moment
	// that request finishes or the browser tab navigates away. refreshMu
	// means several such requests share one refresh; without this, whoever
	// cancels first would cancel it for everyone and mark mlaas unreachable
	// for the rest of statusTTL even though mlaas itself was fine. Every
	// call below carries its own timeout (callTimeout / uploadTimeout), so
	// dropping the caller's cancellation cannot make refresh hang.
	ctx = context.WithoutCancel(ctx)
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.Lock()
	fresh := !force && !s.refreshedAt.IsZero() && s.now().Sub(s.refreshedAt) < statusTTL
	s.mu.Unlock()
	if fresh {
		return
	}

	version, err := s.client.Healthz(ctx)
	if err != nil {
		s.markUnreachable(err)
		return
	}
	s.noteVersion(version)
	datasets, err := s.client.ListDatasets(ctx)
	if err != nil {
		s.markUnreachable(err)
		return
	}
	models, err := s.client.ListModels(ctx)
	if err != nil {
		s.markUnreachable(err)
		return
	}
	rowsByDataset := map[string]int{}
	for _, d := range datasets {
		rowsByDataset[d.Name] = d.Rows
	}
	byName := map[string]wireModel{}
	for _, m := range models {
		byName[m.Name] = m
	}
	healths := map[string]wireHealth{}
	for _, m := range managed {
		name := m.name(s.cfg.Prefix)
		if _, ok := byName[name]; !ok {
			continue
		}
		h, err := s.client.GetHealth(ctx, name)
		if err != nil {
			s.markUnreachable(err)
			return
		}
		healths[name] = h
	}
	jobs, err := s.client.ListJobs(ctx, jobsLimit)
	if err != nil {
		s.markUnreachable(err)
		return
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Reachable = true
	s.snap.CheckedAt = now
	s.snap.Models = s.buildModelsLocked(rowsByDataset, byName, healths)
	s.snap.Jobs = s.managedJobs(jobs)
	s.refreshErr = ""
	s.snap.LastError = s.lastErrorLocked()
	s.refreshedAt = now
}

// buildModelsLocked derives every managed model's status from what a
// refresh read and what the last pass exported. Caller holds mu.
func (s *Syncer) buildModelsLocked(rowsByDataset map[string]int, byName map[string]wireModel, healths map[string]wireHealth) []ModelStatus {
	out := make([]ModelStatus, 0, len(managed))
	for _, m := range managed {
		name := m.name(s.cfg.Prefix)
		ms := s.staticStatus(m)
		rows, dsKnown := rowsByDataset[ms.Dataset]
		ms.DatasetRows = rows
		wm, known := byName[name]
		if known {
			h := healths[name]
			c := h.check()
			champion := h.Champion
			if champion == nil {
				champion = wm.Champion
			}
			if champion != nil {
				ms.Champion = champion.Number
				ms.Holdout = champion.holdout(m.metric)
			}
			if ms.Holdout == nil {
				ms.Holdout = c.HoldoutMetric
			}
			ms.Live = c.WindowMetric
			ms.LiveWindow = c.WindowN
			ms.NewLabels = c.NewLabels
			ms.DriftThreshold = wm.Spec.Retrain.DriftThreshold
			if c.Drift != nil {
				dm := c.DriftMax
				ms.DriftMax = &dm
				ms.DriftFeature = c.DriftFeature
			}
			ms.Note = c.Note
			ms.ActiveJob = h.ActiveJob
			ms.PredictionsLogged = h.PredictionsLogged
			if h.LastRetrainAt != nil {
				ms.LastRetrainAt = *h.LastRetrainAt
			}
		}
		ms.State = deriveState(s.exported[name], dsKnown, known, ms.Champion > 0, ms.ActiveJob)
		out = append(out, ms)
	}
	return out
}

// deriveState is the one place a model's State is decided, so the page
// never has to infer it from the other fields.
func deriveState(exported, datasetKnown, modelKnown, hasChampion, activeJob bool) string {
	switch {
	case modelKnown && hasChampion:
		return StateReady
	case modelKnown && activeJob:
		return StateTraining
	case modelKnown:
		return StateNoChampion
	case datasetKnown || exported:
		return StateMissing
	default:
		return StateWaitingForData
	}
}

// managedJobs keeps the newest jobsShown jobs of this agent's models, in
// the order mlaas listed them (newest first). Job is wireJob field for
// field, only with the dashboard's camelCase tags, so the conversion is the
// whole mapping.
func (s *Syncer) managedJobs(jobs []wireJob) []Job {
	mine := map[string]bool{}
	for _, m := range managed {
		mine[m.name(s.cfg.Prefix)] = true
	}
	out := make([]Job, 0, jobsShown)
	for _, j := range jobs {
		if !mine[j.Model] {
			continue
		}
		out = append(out, Job(j))
		if len(out) == jobsShown {
			break
		}
	}
	return out
}

// Train queues a training job for a managed model.
func (s *Syncer) Train(ctx context.Context, name string) (JobRef, error) {
	return s.enqueue(ctx, name, (*Client).Train)
}

// Tune queues a configuration search for a managed model.
func (s *Syncer) Tune(ctx context.Context, name string) (JobRef, error) {
	return s.enqueue(ctx, name, (*Client).Tune)
}

func (s *Syncer) enqueue(ctx context.Context, name string, call func(*Client, context.Context, string) (JobRef, error)) (JobRef, error) {
	if !s.configured {
		return JobRef{}, ErrNotConfigured
	}
	if _, ok := lookup(s.cfg.Prefix, name); !ok {
		return JobRef{}, ErrUnknownModel
	}
	ref, err := call(s.client, ctx, name)
	if err != nil {
		return JobRef{}, err
	}
	// The next poll should show the job, not a snapshot from before it.
	s.mu.Lock()
	s.refreshedAt = time.Time{}
	s.mu.Unlock()
	return ref, nil
}

// Predict proxies a prediction to a managed model. The limits are the
// dashboard's, not mlaas's: a handful of rows and message-sized cells, so
// the agent's route cannot be used to push arbitrary payloads at mlaas
// with the agent's key.
func (s *Syncer) Predict(ctx context.Context, name string, rows []map[string]any) (PredictResult, error) {
	if !s.configured {
		return PredictResult{}, ErrNotConfigured
	}
	if _, ok := lookup(s.cfg.Prefix, name); !ok {
		return PredictResult{}, ErrUnknownModel
	}
	if len(rows) == 0 {
		return PredictResult{}, &UpstreamError{Status: 400, Message: "rows must not be empty"}
	}
	if len(rows) > maxPredictRows {
		return PredictResult{}, &UpstreamError{Status: 400, Message: fmt.Sprintf("at most %d rows per request", maxPredictRows)}
	}
	for i, r := range rows {
		for k, v := range r {
			if str, ok := v.(string); ok && len(str) > maxMessageBytes {
				return PredictResult{}, &UpstreamError{Status: 400, Message: fmt.Sprintf("row %d: %q is longer than %d bytes", i, k, maxMessageBytes)}
			}
		}
	}
	resp, err := s.client.Predict(ctx, name, rows)
	if err != nil {
		return PredictResult{}, err
	}
	out := PredictResult{Model: resp.Model, Version: resp.Version, Predictions: make([]PredictedRow, len(resp.Predictions))}
	for i, p := range resp.Predictions {
		out.Predictions[i] = PredictedRow{Value: p.Value, Unusual: p.Unusual}
	}
	return out, nil
}
