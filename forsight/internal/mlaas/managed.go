package mlaas

// The managed models: exactly these four, in this order. The table is data
// rather than code because everything else in the package — the exporter,
// the sync pass, the status snapshot, the proxy routes' allow-list — walks
// it, and the dashboard shows the rows in this order whether or not mlaas
// has heard of them yet.
//
// Names are "<prefix>-<suffix>" so several agents can share one mlaas
// server without treading on each other's datasets; the prefix is
// Config.Prefix.

const (
	// DefaultPrefix names everything this agent creates in mlaas when
	// Config.Prefix is empty.
	DefaultPrefix = "forsight"

	taskForecast       = "forecast"
	taskClassification = "classification"

	// forsightTag is the free-form tag every spec carries so the mlaas
	// console can group this agent's models. It is the product name, not the
	// prefix: two agents with different prefixes still belong to one group.
	forsightTag = "forsight"
)

// managedModel is one row of the table: what the model is called, what it
// trains on, and how the exporter feeds it.
type managedModel struct {
	// suffix and datasetSuffix follow the prefix: "cpu-forecast" and
	// "host-cpu" become "forsight-cpu-forecast" and "forsight-host-cpu".
	suffix        string
	datasetSuffix string
	plugin        string
	task          string
	// metric is the score mlaas reports for the model ("rmse", "accuracy").
	metric string
	// job is the one-sentence answer to "what does this model do", shown on
	// the Models page next to Forseer's cards.
	job string
	// reads describes the exporter's input in the agent's own vocabulary.
	reads []string
	// sourceMetric is the agent metric a forecast dataset is bucketed from;
	// empty for the classifier, which reads log lines instead.
	sourceMetric string
}

// managed is the table. Order matters: it is the order the dashboard lists
// the models in and the order the sync pass visits them.
var managed = []managedModel{
	{
		suffix: "cpu-forecast", datasetSuffix: "host-cpu",
		plugin: "holtwinters", task: taskForecast, metric: "rmse",
		job:          "Say where host CPU is heading over the next hour.",
		reads:        []string{"host.cpu.percent, one-minute means"},
		sourceMetric: "host.cpu.percent",
	},
	{
		suffix: "memory-forecast", datasetSuffix: "host-memory",
		plugin: "holtwinters", task: taskForecast, metric: "rmse",
		job:          "Say where host memory is heading over the next hour.",
		reads:        []string{"host.memory.percent, one-minute means"},
		sourceMetric: "host.memory.percent",
	},
	{
		suffix: "disk-forecast", datasetSuffix: "host-disk",
		plugin: "holtwinters", task: taskForecast, metric: "rmse",
		job:          "Say where disk usage is heading, and so when it fills.",
		reads:        []string{"host.disk.percent, one-minute means"},
		sourceMetric: "host.disk.percent",
	},
	{
		suffix: "log-severity", datasetSuffix: "logs",
		plugin: "bayes", task: taskClassification, metric: "accuracy",
		job: "Give a log line the level this deployment would have given it — " +
			"the same job as Forseer's in-binary model, served with a real holdout score.",
		reads: []string{"log lines whose source declared a level (never an inferred one)"},
	},
}

// name is the model's name in mlaas for a prefix.
func (m managedModel) name(prefix string) string { return prefix + "-" + m.suffix }

// dataset is the model's dataset name in mlaas for a prefix.
func (m managedModel) dataset(prefix string) string { return prefix + "-" + m.datasetSuffix }

// spec is the recipe POST /models takes for this model. Only the policy
// fields the agent has an opinion about are set; mlaas fills the rest with
// its defaults, and re-applies them every time the spec is read back, so
// there is no point pinning values we do not mean.
//
// Forecasts retrain on a schedule: the series grows by one row a minute and
// there are no labels to count, so "every 30 minutes, at most one job per
// 10" is what keeps the champion current. The classifier retrains on
// labels: the feedback loop posts the declared level back for every line
// it sends, so min_new_labels is the trigger that means something there.
func (m managedModel) spec(prefix string) wireSpec {
	s := wireSpec{
		Name:    m.name(prefix),
		Dataset: m.dataset(prefix),
		Plugin:  m.plugin,
		Task:    m.task,
		Metric:  m.metric,
		Params:  map[string]any{},
		Tags:    []string{forsightTag},
	}
	switch m.task {
	case taskForecast:
		s.Target = forecastValueColumn
		s.Timestamp = forecastTimeColumn
		s.Retrain = wireRetrain{ScheduleMinutes: 30, CooldownMinutes: 10}
	case taskClassification:
		s.Target = severityLabelColumn
		s.Features = []string{severityTextColumn}
		s.Retrain = wireRetrain{MinNewLabels: 50, CooldownMinutes: 10}
	}
	return s
}

// lookup finds the managed model behind a full name, for the proxy routes'
// allow-list. It reports false for any name this agent does not manage —
// including another tenant's models on the same server.
func lookup(prefix, name string) (managedModel, bool) {
	for _, m := range managed {
		if m.name(prefix) == name {
			return m, true
		}
	}
	return managedModel{}, false
}

// Names lists the managed models' full names for a prefix, in table order.
// An empty prefix means DefaultPrefix.
func Names(prefix string) []string {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	out := make([]string, len(managed))
	for i, m := range managed {
		out[i] = m.name(prefix)
	}
	return out
}
