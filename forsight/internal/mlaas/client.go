package mlaas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Client is a thin HTTP client for mlaas's API. It mirrors the wire shapes
// exactly and applies no policy: a 409 from POST /models is returned as an
// *UpstreamError, and it is the sync pass that decides a 409 means "already
// there, carry on". Keeping the two apart is what lets client_test.go pin
// the wire format and sync_test.go pin the behaviour separately.
type Client struct {
	// BaseURL is the server, without a trailing slash: http://127.0.0.1:8090.
	BaseURL string
	// APIKey goes out as X-API-Key on every call except Healthz.
	APIKey string
	// HTTP is the transport. Its own Timeout stays zero: each call sets a
	// context deadline instead (callTimeout, uploadTimeout), so a large
	// upload gets longer than a health probe without two clients.
	HTTP *http.Client
}

const (
	// callTimeout bounds every call but the upload. Nothing here is
	// interactive except the proxied Predict, and 10s is long enough for
	// mlaas to start a cold plugin process for it.
	callTimeout = 10 * time.Second
	// uploadTimeout bounds POST /datasets: two weeks of one-minute rows is
	// under a megabyte, but mlaas re-reads the file to count rows and
	// columns before it answers.
	uploadTimeout = 60 * time.Second
	// maxResponseBytes caps what a response body may grow to in memory. The
	// largest legitimate answer is a model list with champion metrics, well
	// under this; anything bigger is a misconfigured URL pointing at
	// something that is not mlaas.
	maxResponseBytes = 8 << 20
	// maxErrorBodyBytes caps a non-2xx body: mlaas's own errors are a short
	// {"error": "..."} object, and a body this large is either a proxy's
	// HTML page or something pathological — either way maxResponseBytes'
	// 8MiB has no reason to be spent reading it before errorMessage trims
	// whatever it finds down further still.
	maxErrorBodyBytes = 64 << 10
	// maxErrorMessageBytes bounds what mlaas's own error text contributes
	// to an UpstreamError. The dashboard renders Message directly, so an
	// upstream that answers with an unbounded string should not be able to
	// push an unbounded string onto the page.
	maxErrorMessageBytes = 512
)

// noRedirectClient is the transport NewClient hands out, and the fallback
// do() uses when a caller sets Client.HTTP to nil — never http.DefaultClient,
// whose default CheckRedirect follows a 3xx and replays every request
// header, X-API-Key included, on whatever host it points to. mlaas has no
// reason to answer with a redirect; treating one as the final response
// turns a surprising credential leak into an ordinary *UpstreamError.
var noRedirectClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// NewClient builds a Client for a base URL and key. The URL is used as
// given except for a trailing slash; New validates it before this is
// called.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    noRedirectClient,
	}
}

// ---- wire shapes: mlaas's JSON, snake_case, nothing added ----

// wireDataset is store.Dataset as GET /datasets and POST /datasets return
// it.
type wireDataset struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Columns   []string  `json:"columns"`
	Rows      int       `json:"rows"`
	CreatedAt time.Time `json:"created_at"`
}

// wireRetrain is the part of store.RetrainPolicy the agent sets. Fields
// left zero are omitted so mlaas applies its own defaults to them.
type wireRetrain struct {
	MinNewLabels    int `json:"min_new_labels,omitempty"`
	CooldownMinutes int `json:"cooldown_minutes,omitempty"`
	ScheduleMinutes int `json:"schedule_minutes,omitempty"`
}

// wireSpec is store.Spec: what POST /models takes and what a listed model
// carries under "spec". Policy fields the agent never sets (promotion) are
// left out and so ignored on the way back.
type wireSpec struct {
	Name      string         `json:"name"`
	Dataset   string         `json:"dataset"`
	Plugin    string         `json:"plugin"`
	Task      string         `json:"task"`
	Target    string         `json:"target"`
	Timestamp string         `json:"timestamp,omitempty"`
	Features  []string       `json:"features,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Params    map[string]any `json:"params"`
	Metric    string         `json:"metric"`
	Retrain   wireRetrain    `json:"retrain"`
}

// wireVersion is store.Version; Metrics is store.VersionMetrics, of which
// only the holdout map is read.
type wireVersion struct {
	ID        int64           `json:"id"`
	Model     string          `json:"model"`
	Number    int             `json:"number"`
	Status    string          `json:"status"`
	Metrics   json.RawMessage `json:"metrics"`
	Reason    string          `json:"reason"`
	TrainedAt time.Time       `json:"trained_at"`
}

// holdout is the version's score on the named metric, nil when the
// metrics blob has none (an unscored or undecodable version).
func (v *wireVersion) holdout(metric string) *float64 {
	if v == nil || len(v.Metrics) == 0 {
		return nil
	}
	var vm struct {
		Holdout map[string]float64 `json:"holdout"`
	}
	if json.Unmarshal(v.Metrics, &vm) != nil {
		return nil
	}
	score, ok := vm.Holdout[metric]
	if !ok {
		return nil
	}
	return &score
}

// wireModel is one item of GET /models: store.Model plus the "champion"
// version the listing joins in. POST /models answers with the same shape
// minus "champion".
type wireModel struct {
	Name            string          `json:"name"`
	Classes         []string        `json:"classes,omitempty"`
	Spec            wireSpec        `json:"spec"`
	ChampionVersion *int64          `json:"champion_version"`
	LastRetrainAt   *time.Time      `json:"last_retrain_at"`
	LastCheck       json.RawMessage `json:"last_check"`
	CreatedAt       time.Time       `json:"created_at"`
	Champion        *wireVersion    `json:"champion,omitempty"`
}

// wireCheck is train.Check, the "last_check" of a model: what the retrain
// loop measured the last time it looked.
type wireCheck struct {
	CheckedAt     time.Time `json:"checked_at"`
	Champion      int       `json:"champion,omitempty"`
	NewLabels     int       `json:"new_labels"`
	WindowN       int       `json:"window_n"`
	WindowMetric  *float64  `json:"window_metric,omitempty"`
	HoldoutMetric *float64  `json:"holdout_metric,omitempty"`
	DriftMax      float64   `json:"drift_max"`
	Note          string    `json:"note,omitempty"`
}

// wireHealth is GET /models/{name}/health.
type wireHealth struct {
	Model             string          `json:"model"`
	Metric            string          `json:"metric"`
	LastCheck         json.RawMessage `json:"last_check"`
	LastRetrainAt     *time.Time      `json:"last_retrain_at"`
	Champion          *wireVersion    `json:"champion,omitempty"`
	ActiveJob         bool            `json:"active_job"`
	PredictionsLogged int             `json:"predictions_logged"`
}

// check decodes last_check; a model that has never been checked has a null
// there, which decodes to the zero value.
func (h *wireHealth) check() wireCheck {
	var c wireCheck
	if len(h.LastCheck) > 0 {
		_ = json.Unmarshal(h.LastCheck, &c)
	}
	return c
}

// wireJob is store.Job without the log tail (mlaas sends it; it is not
// decoded into anything).
type wireJob struct {
	ID         int64      `json:"id"`
	Model      string     `json:"model"`
	Kind       string     `json:"kind"`
	Trigger    string     `json:"trigger"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// wireJobRef is what POST /models/{name}/train and /tune answer (202).
type wireJobRef struct {
	JobID         int64  `json:"job_id"`
	Status        string `json:"status"`
	AlreadyQueued bool   `json:"already_queued"`
}

// wirePrediction is one entry of a predict response.
type wirePrediction struct {
	PredictionID string            `json:"prediction_id"`
	Value        string            `json:"value"`
	Unusual      map[string]string `json:"unusual,omitempty"`
}

// wirePredictResponse is POST /models/{name}/predict's 200 body.
type wirePredictResponse struct {
	Model       string           `json:"model"`
	Version     int              `json:"version"`
	Predictions []wirePrediction `json:"predictions"`
}

// wireLabel is one outcome for POST /feedback.
type wireLabel struct {
	PredictionID string `json:"prediction_id"`
	Label        string `json:"label"`
}

// wireFeedbackResponse is POST /feedback's 200 body.
type wireFeedbackResponse struct {
	Accepted             int      `json:"accepted"`
	UnknownPredictionIDs []string `json:"unknown_prediction_ids"`
}

// wireActual is GET /models/{name}/actual?at=: whether the series has a
// value at that instant, and how many earlier predictions the call labelled
// on the way.
type wireActual struct {
	Model   string `json:"model"`
	At      string `json:"at"`
	Found   bool   `json:"found"`
	Value   string `json:"value,omitempty"`
	Labeled int    `json:"labeled"`
}

// ---- calls ----

// Healthz is the liveness probe. It sends no key, like a supervisor would,
// and reports mlaas's own verdict: a 503 names the part that failed.
func (c *Client) Healthz(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	var body struct {
		OK     bool   `json:"ok"`
		Failed string `json:"failed"`
	}
	if err := c.do(req, &body); err != nil {
		return err
	}
	if !body.OK {
		return fmt.Errorf("mlaas is unhealthy: %s", body.Failed)
	}
	return nil
}

// ListDatasets is GET /datasets.
func (c *Client) ListDatasets(ctx context.Context) ([]wireDataset, error) {
	var out []wireDataset
	return out, c.call(ctx, http.MethodGet, "/datasets", nil, &out)
}

// UploadDataset is POST /datasets: a multipart form with the dataset name
// and the CSV as "file". mlaas replaces an existing dataset of that name
// atomically, which is how the agent feeds it new observations.
func (c *Client) UploadDataset(ctx context.Context, name string, csvBytes []byte) (wireDataset, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("name", name); err != nil {
		return wireDataset{}, err
	}
	part, err := mw.CreateFormFile("file", name+".csv")
	if err != nil {
		return wireDataset{}, err
	}
	if _, err := part.Write(csvBytes); err != nil {
		return wireDataset{}, err
	}
	if err := mw.Close(); err != nil {
		return wireDataset{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/datasets", &buf)
	if err != nil {
		return wireDataset{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var out wireDataset
	return out, c.do(req, &out)
}

// ListModels is GET /models: every model on the server, each with its
// champion version joined in.
func (c *Client) ListModels(ctx context.Context) ([]wireModel, error) {
	var out []wireModel
	return out, c.call(ctx, http.MethodGet, "/models", nil, &out)
}

// GetHealth is GET /models/{name}/health. mlaas refreshes the model's
// check on the way, so the answer is current, not the last tick's.
func (c *Client) GetHealth(ctx context.Context, name string) (wireHealth, error) {
	var out wireHealth
	return out, c.call(ctx, http.MethodGet, "/models/"+url.PathEscape(name)+"/health", nil, &out)
}

// CreateModel is POST /models. A model that already exists comes back as
// *UpstreamError with Status 409; the caller decides what that means.
func (c *Client) CreateModel(ctx context.Context, spec wireSpec) (wireModel, error) {
	var out wireModel
	return out, c.call(ctx, http.MethodPost, "/models", spec, &out)
}

// GetModel is GET /models/{name}: `{"model": {...}, "versions": [...]}`.
// Only the model itself is decoded — the sync pass wants it for one reason,
// to recover Classes (and everything else) after a 409 on create, not for
// the version history alongside it.
func (c *Client) GetModel(ctx context.Context, name string) (wireModel, error) {
	var out struct {
		Model wireModel `json:"model"`
	}
	err := c.call(ctx, http.MethodGet, "/models/"+url.PathEscape(name), nil, &out)
	return out.Model, err
}

// Train is POST /models/{name}/train: queue a training job, or learn which
// one is already waiting.
func (c *Client) Train(ctx context.Context, name string) (JobRef, error) {
	return c.enqueue(ctx, name, "train")
}

// Tune is POST /models/{name}/tune: queue a configuration search that
// promotes its winner only if it beats the champion.
func (c *Client) Tune(ctx context.Context, name string) (JobRef, error) {
	return c.enqueue(ctx, name, "tune")
}

func (c *Client) enqueue(ctx context.Context, name, kind string) (JobRef, error) {
	var out wireJobRef
	if err := c.call(ctx, http.MethodPost, "/models/"+url.PathEscape(name)+"/"+kind, nil, &out); err != nil {
		return JobRef{}, err
	}
	return JobRef{JobID: out.JobID, AlreadyQueued: out.AlreadyQueued}, nil
}

// Predict is POST /models/{name}/predict with {"rows": [...]}. A model
// without a champion answers 409, returned as *UpstreamError.
func (c *Client) Predict(ctx context.Context, name string, rows []map[string]any) (wirePredictResponse, error) {
	var out wirePredictResponse
	body := map[string]any{"rows": rows}
	return out, c.call(ctx, http.MethodPost, "/models/"+url.PathEscape(name)+"/predict", body, &out)
}

// Feedback is POST /feedback: the outcomes for earlier predictions, one
// call for a whole batch. mlaas refuses the batch if any label is not one
// of the model's classes, so the caller filters first.
func (c *Client) Feedback(ctx context.Context, labels []wireLabel) (wireFeedbackResponse, error) {
	var out wireFeedbackResponse
	body := map[string]any{"labels": labels}
	return out, c.call(ctx, http.MethodPost, "/feedback", body, &out)
}

// Actual is GET /models/{name}/actual?at=. Its answer is rarely
// interesting; the point of calling it is the side effect: mlaas labels
// every earlier forecast whose instant the series now covers, which is
// what gives a forecast model a live score.
func (c *Client) Actual(ctx context.Context, name string, at time.Time) (wireActual, error) {
	var out wireActual
	q := url.Values{"at": {at.UTC().Format(timeLayout)}}
	return out, c.call(ctx, http.MethodGet, "/models/"+url.PathEscape(name)+"/actual?"+q.Encode(), nil, &out)
}

// ListJobs is GET /jobs?limit=, newest first, every model.
func (c *Client) ListJobs(ctx context.Context, limit int) ([]wireJob, error) {
	var out []wireJob
	return out, c.call(ctx, http.MethodGet, "/jobs?limit="+strconv.Itoa(limit), nil, &out)
}

// ---- plumbing ----

// call sends one authenticated JSON request under callTimeout and decodes
// the answer into out.
func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, out)
}

// do sends the request with the key attached, turns any non-2xx into an
// *UpstreamError carrying mlaas's own message, and decodes a 2xx body into
// out when there is one to decode into.
func (c *Client) do(req *http.Request, out any) error {
	if c.APIKey != "" && req.URL.Path != "/healthz" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	req.Header.Set("Accept", "application/json")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = noRedirectClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// An error body only ever needs to hold a short {"error": "..."}; a
	// success body is the one that can legitimately run to maxResponseBytes
	// (a full model or job list).
	limit := int64(maxResponseBytes)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		limit = maxErrorBodyBytes
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return fmt.Errorf("%s %s: read response: %w", req.Method, req.URL.Path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &UpstreamError{Status: resp.StatusCode, Message: errorMessage(resp.StatusCode, raw)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, err)
	}
	return nil
}

// errorMessage is the text of an error response: mlaas's {"error": "..."}
// when the body is one, else the status text. Only that field is kept —
// anything else in a non-JSON body (a proxy's HTML page, say) is noise
// that would end up on the dashboard.
func errorMessage(status int, raw []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Error != "" {
		return truncateError(body.Error)
	}
	if text := http.StatusText(status); text != "" {
		return text
	}
	return "status " + strconv.Itoa(status)
}

// truncateError trims s to maxErrorMessageBytes without cutting a UTF-8
// sequence in half, appending an ellipsis when it cuts anything — the one
// place an upstream error string is shortened before it reaches
// UpstreamError.Message and, from there, the dashboard.
func truncateError(s string) string {
	if len(s) <= maxErrorMessageBytes {
		return s
	}
	cut := maxErrorMessageBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// upstreamStatus is the status an error carries when it is mlaas's own
// answer, or 0 for a transport or decoding failure.
func upstreamStatus(err error) int {
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return ue.Status
	}
	return 0
}
