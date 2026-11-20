package github

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
)

// CancellationRecord is private evidence of a run-scoped cancellation race.
// It is never part of the public result envelope.
type CancellationRecord struct {
	SchemaVersion    int    `json:"schema_version"`
	RunID            string `json:"run_id"`
	OriginalAttempt  int    `json:"original_attempt"`
	RequestedAttempt int    `json:"requested_attempt"`
	ObservedAttempt  int    `json:"observed_attempt"`
	ObservedState    string `json:"observed_state"`
	PostAttempted    bool   `json:"post_attempted"`
	CancelRequested  bool   `json:"cancel_requested"`
	CancelConfirmed  bool   `json:"cancel_confirmed"`
	ErrorCode        string `json:"error_code"`
}

// CancellationRecorder writes one user-only outcome per operation. A persistence
// failure refuses the operation result; it never retries the provider write.
func CancellationRecorder(dir string, x Expectation) (func(CancellationRecord) error, error) {
	if x.Validate() != nil || !config.NoLinks(dir) || !process.UserOnly(dir, true) {
		return nil, Fail(3, "policy_refused")
	}
	id, e := protocol.NewUUID()
	if e != nil {
		return nil, Fail(6, "missing_evidence")
	}
	return func(r CancellationRecord) error {
		if r.SchemaVersion != 1 || r.RunID != x.RunID || r.OriginalAttempt != x.Attempt || r.RequestedAttempt != 0 && r.RequestedAttempt != x.Attempt || r.ObservedAttempt < 0 || !slices.Contains([]string{"unknown", "queued", "in_progress", "passed", "failed", "cancelled"}, r.ObservedState) || !slices.Contains([]string{"", "missing_evidence", "cancel_attempt_drift", "policy_refused", "deadline", "interrupted"}, r.ErrorCode) || r.PostAttempted && r.RequestedAttempt == 0 || r.CancelRequested && !r.PostAttempted || r.CancelConfirmed && (!r.CancelRequested || r.ObservedAttempt != x.Attempt || r.ObservedState != "cancelled") {
			return Fail(6, "missing_evidence")
		}
		p, e := config.SafePath(dir, x.RunID+".cancel."+id+".json")
		if e != nil || !process.UserOnly(dir, true) {
			return Fail(3, "policy_refused")
		}
		b, e := config.Canonical(struct {
			CancellationRecord
			OperationID   string    `json:"operation_id"`
			RequestID     string    `json:"request_id"`
			RequestDigest string    `json:"request_digest"`
			RecordedAt    time.Time `json:"recorded_at"`
		}{r, id, x.Dispatch.Inputs["request_id"], x.Dispatch.Inputs["request_digest"], time.Now().UTC()})
		if e != nil {
			return Fail(6, "missing_evidence")
		}
		f, e := process.CreatePrivateFile(filepath.Clean(p))
		if e != nil {
			return Fail(3, "policy_refused")
		}
		_, writeErr := f.Write(b)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return Fail(6, "missing_evidence")
		}
		return nil
	}, nil
}
