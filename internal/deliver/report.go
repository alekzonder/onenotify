// Package deliver is one pass of onenotify's work: take events from the
// sources into the per-channel queues, then deliver each queue in order.
package deliver

// Outcome says what happened to a message in this run.
type Outcome string

// Outcomes.
const (
	Sent    Outcome = "sent"    // the provider accepted it; it left the queue
	Waiting Outcome = "waiting" // not delivered; it stays in the queue for the next run
	Failed  Outcome = "failed"  // it will not be delivered; it was moved to the failed store
)

// Record is one line of a run's report: a message that was attempted, or
// that left the queue without an attempt.
type Record struct {
	Channel    string  `json:"channel"`
	Module     string  `json:"module"`
	ID         string  `json:"id"`
	Outcome    Outcome `json:"outcome"`
	DurationMS int64   `json:"duration_ms"`
	// Error says why the message was not sent; empty for a sent message.
	Error string `json:"error,omitempty"`
}

// Report is the result of a run.
type Report struct {
	// Ingested is the number of events taken from the sources.
	Ingested int `json:"ingested"`
	// Records are ordered by channel name, then by message.
	Records []Record `json:"records"`
	Sent    int      `json:"sent"`
	// Waiting counts every message left in the queues that were served,
	// including those behind a failed one, which have no record.
	Waiting int `json:"waiting"`
	Failed  int `json:"failed"`
}

// Total is the number of messages the run dealt with.
func (r *Report) Total() int { return r.Sent + r.Waiting + r.Failed }
