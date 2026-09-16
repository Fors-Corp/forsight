package forseer

import "time"

// Severity of an insight. Matches the design-system AlertList vocabulary so
// the dashboard can render these without a translation layer.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Kind names the detector that produced an insight. The dashboard maps these
// onto specific design-system components (see README).
const (
	KindAnomaly     = "anomaly"      // AlertList — rolling z-score
	KindChangepoint = "changepoint"  // Timeline — CUSUM regime shift
	KindLogBurst    = "log_burst"    // BarList + LogStream — template volume spike
	KindSlowSpan    = "slow_span"    // TraceWaterfall — duration outlier
	KindCulprit     = "culprit"      // Table — process that jumped with host CPU
	KindHostOutlier = "host_outlier" // AlertList — Mahalanobis distance over the host vector
)

// Insight is one Forseer finding. The agent serves these at GET /api/v1/forseer/insights.
type Insight struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Severity    string    `json:"severity"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	Metric      string    `json:"metric,omitempty"`
	Value       float64   `json:"value,omitempty"`
	Time        time.Time `json:"time"`
	Related     []string  `json:"related,omitempty"`
}

// Cluster is one Drain-style log template. GET /api/v1/forseer/clusters.
type Cluster struct {
	ID         string    `json:"id"`
	Template   string    `json:"template"`
	Source     string    `json:"source"`
	Count      int       `json:"count"`
	ErrorCount int       `json:"errorCount"`
	LastSeen   time.Time `json:"lastSeen"`
	Sample     string    `json:"sample"`
	// PagingScore is the paging model's probability that a burst of this
	// template right now is worth paging for. Present only while that model
	// is ready; absent means "use the count", which is what the volume rule
	// amounts to.
	PagingScore *float64 `json:"pagingScore,omitempty"`
}

// Point is the tiny metric shape Forseer needs. Defined here so this module
// does not import the agent's internal/model (the agent depends on Forseer,
// not the other way around).
type Point struct {
	Name   string
	Value  float64
	Labels map[string]string
}

// LogLine is one ingested log, stripped to what the miner needs.
type LogLine struct {
	Timestamp time.Time
	Severity  string
	Source    string
	Message   string
	// SeverityInferred is true when nothing in the source declared a level
	// and the agent guessed one — a tailed file, today. The severity model
	// trains only on declared levels: learning from a guess would teach it
	// the rule it exists to replace.
	SeverityInferred bool
}

// SpanSample is one ingested span, stripped to what the slow-span watcher needs.
type SpanSample struct {
	Name       string
	Service    string
	DurationMs float64
	Status     string
	TraceID    string
	SpanID     string
	ParentID   string
}

// Budget is an error-log SLO read for ErrorBudget. GET /api/v1/forseer/budget.
type Budget struct {
	Label    string  `json:"label"`
	Consumed float64 `json:"consumed"`
	Caption  string  `json:"caption"`
	// Forecast is when the budget runs out, as a range, or empty when the
	// burn is not on course to exhaust it or the model cannot say yet. The
	// same words are appended to Caption, so a dashboard that knows nothing
	// about this field still shows the projection.
	Forecast  string  `json:"forecast,omitempty"`
	Errors    int     `json:"errors"`
	Total     int     `json:"total"`
	SLO       float64 `json:"slo"`
	WarningAt float64 `json:"warningAt"`
	DangerAt  float64 `json:"dangerAt"`
}

// Event is one stitch on the incident Timeline. GET /api/v1/forseer/timeline.
type Event struct {
	ID          string    `json:"id"`
	Time        time.Time `json:"time"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Tone        string    `json:"tone"`
}

// Facet is one FilterBar chip. GET /api/v1/forseer/query?q=.
type Facet struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}
