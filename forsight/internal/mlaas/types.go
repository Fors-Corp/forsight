// Package mlaas connects the agent to a running mlaas — Marc's local
// ML-as-a-Service (github.com/marcfs31/mlaas): one Go binary that trains,
// tunes, serves and retrains models behind an HTTP API with an X-API-Key.
//
// The division of labour with Forseer is deliberate. Forseer (../../forseer)
// is the stdlib-only module compiled into this binary: its models train
// online on the ingest path, gate on a fallback, and carry no weights. mlaas
// is the opposite trade: a separate service with real holdouts, a
// champion/challenger loop, tuning and drift, that keeps a growing training
// corpus. This package feeds it the agent's own stream and reads back what
// it learned, so an operator sees both kinds of model on one page.
//
// What it does, on a timer (Config.SyncInterval):
//
//   - exports datasets from the store — one-minute buckets of the host
//     series for the forecasts, declared-severity log lines for the
//     classifier — and uploads each whenever its row count changed;
//   - registers a model spec per dataset (a 409 means it already exists)
//     and enqueues a training job while the model has no champion;
//   - asks each forecast champion for the next hour and caches the answer
//     for the dashboard, then tells mlaas the observed values so the
//     forecasts are scored live;
//   - feeds the severity champion a sample of lines that arrived with a
//     declared level, then posts that level back as the outcome, which is
//     what makes mlaas's live accuracy and its retrain triggers real.
//
// The API key never leaves this process: the dashboard talks to the agent's
// own /api/v1/mlaas/* routes (behind the agent's bearer auth, when set), and
// the agent talks to mlaas.
package mlaas

import (
	"errors"
	"fmt"
	"time"
)

// Config is everything the agent needs to reach mlaas. A zero URL disables
// the integration entirely: nothing is exported and the status reports
// Configured=false.
type Config struct {
	// URL is the base URL of the mlaas server, e.g. http://127.0.0.1:8090.
	URL string
	// APIKey is sent as X-API-Key on every request except the liveness
	// probe. Resolved by the CLI from MLAAS_API_KEY or --mlaas-api-key-file
	// (mlaas writes its key to <data>/api_key); never from a flag value, so
	// it does not show up in `ps`.
	APIKey string
	// Prefix names everything this agent creates in mlaas — datasets
	// "<prefix>-host-cpu", models "<prefix>-cpu-forecast" — so several
	// agents can share one server. Must satisfy mlaas's name rule
	// (^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$ for the whole name). Default
	// "forsight".
	Prefix string
	// SyncInterval is how often datasets are exported and the feedback
	// loops run. Default 5 minutes.
	SyncInterval time.Duration
}

// Model states as reported on ModelStatus.State. The dashboard renders
// each as a Badge and never has to infer one from the other fields.
const (
	// StateWaitingForData: the store does not yet hold enough rows to
	// register a dataset, so nothing has been sent to mlaas for this model.
	StateWaitingForData = "waiting-for-data"
	// StateMissing: the dataset is in mlaas but the model is not (a create
	// failed, or someone deleted it); the next sync pass recreates it.
	StateMissing = "missing"
	// StateTraining: no champion yet, and a job is queued or running.
	StateTraining = "training"
	// StateNoChampion: no champion and no job — the last training failed.
	// The job list on the same page shows why.
	StateNoChampion = "no-champion"
	// StateReady: a champion is serving predictions.
	StateReady = "ready"
)

// Status is the snapshot GET /api/v1/mlaas/status serves — one poll gives
// the dashboard everything its Models page shows about mlaas.
type Status struct {
	// Configured is false when no URL was given; every other field is then
	// empty and the page explains how to turn the integration on.
	Configured bool `json:"configured"`
	// URL is the configured base URL, shown so an operator knows which
	// server this is.
	URL string `json:"url,omitempty"`
	// Reachable reports the last liveness probe (GET /healthz, no auth).
	Reachable bool      `json:"reachable"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	// LastSync is when the last export/feedback pass finished, successful
	// or not; LastError is the pass's or the refresh's error, if any.
	LastSync  time.Time `json:"lastSync,omitzero"`
	LastError string    `json:"lastError,omitempty"`
	// Models is one entry per managed model, in a fixed order, whether or
	// not mlaas knows about it yet (see State).
	Models []ModelStatus `json:"models"`
	// Forecasts is the latest projection per forecast model with a
	// champion, as cached by the last sync pass.
	Forecasts []Forecast `json:"forecasts"`
	// Predictions is the most recent lines the severity feedback loop sent
	// through mlaas, newest first, with what each model answered.
	Predictions []Prediction `json:"predictions"`
	// Jobs is mlaas's recent job list for the managed models, newest first.
	Jobs []Job `json:"jobs"`
}

// ModelStatus describes one managed model the way forseer.Card describes an
// in-binary one: the job in a sentence, what it reads, and how it scores —
// here from mlaas's own holdout and live window rather than a prequential
// count.
type ModelStatus struct {
	// Name is the model's name in mlaas, e.g. "forsight-cpu-forecast".
	Name string `json:"name"`
	// Job is one sentence: what question this model answers.
	Job string `json:"job"`
	// Task is "forecast" or "classification"; Plugin is the mlaas plugin
	// the spec names ("holtwinters", "bayes").
	Task   string `json:"task"`
	Plugin string `json:"plugin"`
	// Dataset is the mlaas dataset the model trains on and DatasetRows how
	// many rows mlaas holds for it (0 until the first upload).
	Dataset     string `json:"dataset"`
	DatasetRows int    `json:"datasetRows"`
	// Reads lists what the exporter feeds the dataset from, in the agent's
	// own vocabulary ("host.cpu.percent, one-minute means").
	Reads []string `json:"reads"`
	// State is one of the State* constants.
	State string `json:"state"`
	// Champion is the serving version number, 0 while there is none.
	Champion int `json:"champion"`
	// Metric names the score below ("rmse", "accuracy").
	Metric string `json:"metric"`
	// Holdout is the champion's score on mlaas's held-out rows; Live is the
	// same metric over the last LiveWindow labelled predictions. Nil when
	// mlaas has not computed one.
	Holdout    *float64 `json:"holdout,omitempty"`
	Live       *float64 `json:"live,omitempty"`
	LiveWindow int      `json:"liveWindow"`
	// NewLabels is how many outcomes arrived since the champion trained —
	// the count mlaas's min_new_labels retrain trigger watches.
	NewLabels int `json:"newLabels"`
	// DriftMax is the largest per-feature population-stability index mlaas
	// measured on recent inputs against the training profile. Nil until
	// mlaas has enough recent predictions to measure it — a live window
	// too small for drift is a "not enough data yet" card, never a
	// flattering zero (MODELS.md).
	DriftMax *float64 `json:"driftMax,omitempty"`
	// DriftFeature names the feature DriftMax came from. Empty whenever
	// DriftMax is nil.
	DriftFeature string `json:"driftFeature,omitempty"`
	// DriftThreshold is the value mlaas itself retrains on
	// (retrain.drift_threshold, default 0.2) — what the Badge compares
	// DriftMax against, not a value this agent chooses.
	DriftThreshold float64 `json:"driftThreshold"`
	// ActiveJob is true while a train or tune job is queued or running.
	ActiveJob bool `json:"activeJob"`
	// PredictionsLogged is how many predictions mlaas has served for this
	// model in total.
	PredictionsLogged int       `json:"predictionsLogged"`
	LastRetrainAt     time.Time `json:"lastRetrainAt,omitzero"`
	// Note is mlaas's own last-check note, when it has one ("no champion
	// yet; train manually first").
	Note string `json:"note,omitempty"`
}

// Forecast is one model's projection of one host series.
type Forecast struct {
	Model string `json:"model"`
	// Metric is the agent's metric name the series comes from, e.g.
	// "host.cpu.percent", so the dashboard can draw the history under it.
	Metric string `json:"metric"`
	// Origin is the last observed one-minute bucket the projection starts
	// from.
	Origin time.Time       `json:"origin"`
	Points []ForecastPoint `json:"points"`
}

// ForecastPoint is one projected value.
type ForecastPoint struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

// Prediction is one log line the feedback loop sent through the severity
// champion, alongside the level the source declared (the truth mlaas was
// then told) and what the in-binary Forseer model said about the same line.
type Prediction struct {
	At       time.Time `json:"at"`
	Message  string    `json:"message"`
	Declared string    `json:"declared"`
	// Forseer is empty when the in-binary model was not ready to answer.
	Forseer string `json:"forseer,omitempty"`
	Mlaas   string `json:"mlaas"`
}

// Job mirrors mlaas's job row, without the log tail.
type Job struct {
	ID         int64      `json:"id"`
	Model      string     `json:"model"`
	Kind       string     `json:"kind"`
	Trigger    string     `json:"trigger"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// JobRef is what POST train/tune answer: the job mlaas queued, or the one
// already waiting when a repeat click arrives.
type JobRef struct {
	JobID         int64 `json:"jobId"`
	AlreadyQueued bool  `json:"alreadyQueued"`
}

// PredictResult is the proxied answer of POST /models/{name}/predict.
type PredictResult struct {
	Model   string `json:"model"`
	Version int    `json:"version"`
	// Predictions is one entry per input row, in order.
	Predictions []PredictedRow `json:"predictions"`
}

// PredictedRow is one prediction. Value is a string because that is what
// mlaas serves — a class name, or a number rendered with strconv 'g'.
type PredictedRow struct {
	Value string `json:"value"`
	// Unusual names each feature whose value looked unlike the training
	// data (a number outside the training range, a category never seen)
	// and why — mlaas's per-prediction drift flag.
	Unusual map[string]string `json:"unusual,omitempty"`
}

// Sentinel errors the API layer maps to status codes.
var (
	// ErrNotConfigured: no mlaas URL was given.
	ErrNotConfigured = errors.New("mlaas: not configured")
	// ErrUnknownModel: the name is not one this agent manages. The proxy
	// routes refuse anything else, so the dashboard cannot reach models
	// other tenants of the same mlaas created.
	ErrUnknownModel = errors.New("mlaas: not a model this agent manages")
)

// UpstreamError is an error mlaas itself answered, with its status code,
// so the API layer can pass a 409 ("model has no trained version yet") or
// a 400 through as-is instead of collapsing everything into a 502.
type UpstreamError struct {
	Status  int
	Message string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("mlaas answered %d: %s", e.Status, e.Message)
}
