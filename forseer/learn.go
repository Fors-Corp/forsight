package forseer

// A Model is one trained tool inside Forseer.
//
// The rule every model here follows: it does exactly one job, it learns that
// job from this deployment's own stream, and it reads only the inputs it
// declares. Nothing arrives pre-trained. There are no weights in this repo
// and no weights in the binary — a model that has never run knows nothing,
// and a model that has run knows only what this installation showed it.
//
// That is the point. A general model carries a great deal of information
// about everything, most of which is wrong here: someone else's log
// vocabulary, someone else's traffic shape, someone else's idea of what a
// slow endpoint looks like. A model trained on one job against one stream
// carries almost none of it.
//
// Three properties keep that honest, and every model must have all three:
//
//   - A declared input list. `Reads` is the whole list, not a summary. A
//     model that looks at something it did not declare is a bug.
//   - A readiness gate. Until a model has seen enough of this deployment to
//     beat the thing it replaces, it says so and the caller uses the
//     fallback. A cold model never degrades the product.
//   - A measured score. Where the stream supplies its own labels, the model
//     grades itself on data it has not yet trained on, so `Accuracy` is
//     something it earned rather than something it was told.
type Model interface {
	// Card describes the model: its job, its inputs, and how it is doing.
	Card() Card

	// Snapshot serializes everything about this model worth carrying across
	// a restart: the learned state, and how much of it there is. The
	// payload is this model's own JSON document, carrying its own schema
	// version so a later change to what a model persists can tell an old
	// snapshot apart from a new one rather than misreading it.
	//
	// It deliberately does not include the transient bookkeeping that
	// exists only to grade the model against its own fallback in real time
	// — see Restore for which fields that is, model by model.
	//
	// Snapshot is not itself the file on disk: Engine.Snapshot
	// (engine.go/persist.go) wraps every model's payload into one document,
	// and forsight/cmd/run.go is what writes that beside the Badger
	// directory under --data-dir.
	Snapshot() ([]byte, error)

	// Restore replaces this model's learned state from a payload Snapshot
	// produced. It is for a cold model at startup, never a warm one: it
	// overwrites rather than merges, so calling it on a model that has
	// already observed live data would discard that observation.
	//
	// A payload whose version does not match this model's current schema,
	// or that does not parse as this model's shape at all, is an error: the
	// caller discards it and the model is left exactly as its constructor
	// built it — never partially applied, never misread as a different
	// shape.
	//
	// What actually comes back, and what is deliberately reset to zero
	// instead — usually the window a model grades itself against its
	// fallback in, so readiness is re-earned on live data rather than
	// carried over from a previous run — is documented on each model's own
	// Restore.
	Restore([]byte) error
}

// Card is the self-description of one model. GET /api/v1/forseer/models
// returns these, which is the only place the agent claims anything about
// what it has learned.
type Card struct {
	// Name is the job, not the method — "log severity", not "naive Bayes".
	// The method is an implementation detail that can change without the
	// dashboard or the operator caring.
	Name string `json:"name"`
	// Job is one sentence: what question this model answers.
	Job string `json:"job"`
	// Reads is every input the model consumes, and nothing else is consumed.
	Reads []string `json:"reads"`
	// Fallback is what the agent uses while the model is not ready. Naming
	// it is part of the contract: a model with no fallback cannot be gated.
	Fallback string `json:"fallback"`
	// Ready reports whether the model has seen enough of this deployment to
	// be trusted over Fallback.
	Ready bool `json:"ready"`
	// Trained is how many labelled examples the model has learned from.
	Trained int `json:"trained"`
	// Accuracy is the share of the most recent predictions that were right,
	// each scored before the model trained on that example. Negative means
	// the job has no labels to grade against, not that the model is bad.
	Accuracy float64 `json:"accuracy"`
	// FallbackAccuracy is Fallback's score over the same window, on the same
	// examples. It is what makes Accuracy mean something: a model at 82% is
	// worth having against a rule at 60% and worth removing against a rule
	// at 90%. Unmeasured when the model has no fallback to compare against.
	FallbackAccuracy float64 `json:"fallbackAccuracy"`
	// Graded is how many predictions the two accuracies are computed over.
	Graded int `json:"graded"`
	// Detail is one line of whatever this model is actually doing, for the
	// models that have something worth saying that Accuracy cannot carry —
	// a calibration has no labels to be right about, but it does have a
	// budget and a rate it is hitting. Optional.
	Detail string `json:"detail,omitempty"`
}

// Unmeasured is the Accuracy of a model whose job supplies no labels to
// grade against. It is deliberately not zero: zero is a real, terrible score.
const Unmeasured = -1.0
