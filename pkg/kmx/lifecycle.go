package kmx

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type State string

const (
	StateQueued         State = "queued"
	StatePreparing      State = "preparing"
	StateRunning        State = "running"
	StateFinalizing     State = "finalizing"
	StateSucceeded      State = "succeeded"
	StateFailed         State = "failed"
	StateCancelled      State = "cancelled"
	StateOutcomeUnknown State = "outcome_unknown"
	StateDeleted        State = "deleted"
)

func (s State) IsFinal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled, StateOutcomeUnknown, StateDeleted:
		return true
	default:
		return false
	}
}

type ExecutionHandle struct {
	id              string
	target          TargetRef
	specDigest      Digest
	bindingDigest   Digest
	contractVersion ContractVersion
	submittedAt     time.Time
}

type executionHandleJSON struct {
	ID              string          `json:"id"`
	Target          TargetRef       `json:"target"`
	SpecDigest      Digest          `json:"specDigest"`
	BindingDigest   Digest          `json:"bindingDigest"`
	ContractVersion ContractVersion `json:"contractVersion"`
	SubmittedAt     time.Time       `json:"submittedAt"`
}

func NewExecutionHandle(
	id string,
	target TargetRef,
	specDigest Digest,
	bindingDigest Digest,
	contractVersion ContractVersion,
	submittedAt time.Time,
) (ExecutionHandle, error) {
	if id == "" {
		return ExecutionHandle{}, &InvalidError{Field: "id", Reason: "must not be empty"}
	}
	if target.ID == "" || target.Class == "" {
		return ExecutionHandle{}, &InvalidError{Field: "target", Reason: "must contain id and class"}
	}
	if specDigest.IsZero() || bindingDigest.IsZero() {
		return ExecutionHandle{}, &InvalidError{Field: "digest", Reason: "must not be empty"}
	}
	if contractVersion == "" {
		return ExecutionHandle{}, &InvalidError{Field: "contractVersion", Reason: "must not be empty"}
	}
	if submittedAt.IsZero() {
		return ExecutionHandle{}, &InvalidError{Field: "submittedAt", Reason: "must not be empty"}
	}
	return ExecutionHandle{
		id:              id,
		target:          target,
		specDigest:      specDigest,
		bindingDigest:   bindingDigest,
		contractVersion: contractVersion,
		submittedAt:     submittedAt.UTC(),
	}, nil
}

func (h ExecutionHandle) ID() string                       { return h.id }
func (h ExecutionHandle) Target() TargetRef                { return h.target }
func (h ExecutionHandle) SpecDigest() Digest               { return h.specDigest }
func (h ExecutionHandle) BindingDigest() Digest            { return h.bindingDigest }
func (h ExecutionHandle) ContractVersion() ContractVersion { return h.contractVersion }
func (h ExecutionHandle) SubmittedAt() time.Time           { return h.submittedAt }

func (h ExecutionHandle) MarshalJSON() ([]byte, error) {
	return json.Marshal(executionHandleJSON{
		ID:              h.id,
		Target:          h.target,
		SpecDigest:      h.specDigest,
		BindingDigest:   h.bindingDigest,
		ContractVersion: h.contractVersion,
		SubmittedAt:     h.submittedAt,
	})
}

func (h *ExecutionHandle) UnmarshalJSON(data []byte) error {
	var value executionHandleJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	decoded, err := NewExecutionHandle(
		value.ID,
		value.Target,
		value.SpecDigest,
		value.BindingDigest,
		value.ContractVersion,
		value.SubmittedAt,
	)
	if err != nil {
		return err
	}
	*h = decoded
	return nil
}

type ExecutionSnapshot struct {
	Handle          ExecutionHandle   `json:"handle"`
	State           State             `json:"state"`
	Sequence        uint64            `json:"sequence"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	Message         string            `json:"message,omitempty"`
	ResultAvailable bool              `json:"resultAvailable,omitempty"`
	Receipt         *ExecutionReceipt `json:"receipt,omitempty"`
}

type Evidence struct {
	Name   string    `json:"name"`
	Digest Digest    `json:"digest"`
	Time   time.Time `json:"time"`
}

type ExecutionReceipt struct {
	executionID   string
	specDigest    Digest
	bindingDigest Digest
	resultDigest  Digest
	evidence      []Evidence
	completedAt   time.Time
}

type executionReceiptJSON struct {
	ExecutionID   string     `json:"executionId"`
	SpecDigest    Digest     `json:"specDigest"`
	BindingDigest Digest     `json:"bindingDigest"`
	ResultDigest  Digest     `json:"resultDigest"`
	Evidence      []Evidence `json:"evidence,omitempty"`
	CompletedAt   time.Time  `json:"completedAt"`
}

func NewExecutionReceipt(
	executionID string,
	specDigest Digest,
	bindingDigest Digest,
	resultDigest Digest,
	evidence []Evidence,
	completedAt time.Time,
) (ExecutionReceipt, error) {
	if executionID == "" {
		return ExecutionReceipt{}, &InvalidError{Field: "executionId", Reason: "must not be empty"}
	}
	if specDigest.IsZero() || bindingDigest.IsZero() || resultDigest.IsZero() {
		return ExecutionReceipt{}, &InvalidError{Field: "digest", Reason: "must not be empty"}
	}
	if completedAt.IsZero() {
		return ExecutionReceipt{}, &InvalidError{Field: "completedAt", Reason: "must not be empty"}
	}
	return ExecutionReceipt{
		executionID:   executionID,
		specDigest:    specDigest,
		bindingDigest: bindingDigest,
		resultDigest:  resultDigest,
		evidence:      append([]Evidence(nil), evidence...),
		completedAt:   completedAt.UTC(),
	}, nil
}

func (r ExecutionReceipt) ExecutionID() string    { return r.executionID }
func (r ExecutionReceipt) SpecDigest() Digest     { return r.specDigest }
func (r ExecutionReceipt) BindingDigest() Digest  { return r.bindingDigest }
func (r ExecutionReceipt) ResultDigest() Digest   { return r.resultDigest }
func (r ExecutionReceipt) CompletedAt() time.Time { return r.completedAt }
func (r ExecutionReceipt) Evidence() []Evidence   { return append([]Evidence(nil), r.evidence...) }

func (r ExecutionReceipt) MarshalJSON() ([]byte, error) {
	return json.Marshal(executionReceiptJSON{
		ExecutionID:   r.executionID,
		SpecDigest:    r.specDigest,
		BindingDigest: r.bindingDigest,
		ResultDigest:  r.resultDigest,
		Evidence:      r.evidence,
		CompletedAt:   r.completedAt,
	})
}

func (r *ExecutionReceipt) UnmarshalJSON(data []byte) error {
	var value executionReceiptJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	decoded, err := NewExecutionReceipt(
		value.ExecutionID,
		value.SpecDigest,
		value.BindingDigest,
		value.ResultDigest,
		value.Evidence,
		value.CompletedAt,
	)
	if err != nil {
		return err
	}
	*r = decoded
	return nil
}

type SubmitOptions struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

type GetOptions struct{}

type WatchOptions struct {
	AfterSequence uint64 `json:"afterSequence,omitempty"`
}

type LogOptions struct {
	AfterSequence uint64 `json:"afterSequence,omitempty"`
}

type OutputOptions struct{}

type ArtifactOptions struct {
	After string `json:"after,omitempty"`
}

type CancelOptions struct {
	Reason string `json:"reason,omitempty"`
}

type DeleteOptions struct{}

type CancelReceipt struct {
	ExecutionID string    `json:"executionId"`
	Accepted    bool      `json:"accepted"`
	Time        time.Time `json:"time"`
}

type DeleteReceipt struct {
	ExecutionID string    `json:"executionId"`
	Deleted     bool      `json:"deleted"`
	Time        time.Time `json:"time"`
}

type Client interface {
	Submit(context.Context, WorkloadSpec, SubmitOptions) (ExecutionHandle, error)
	Get(context.Context, ExecutionHandle, GetOptions) (ExecutionSnapshot, error)
	Watch(context.Context, ExecutionHandle, WatchOptions) (EventStream, error)
	Logs(context.Context, ExecutionHandle, LogOptions) (LogStream, error)
	Output(context.Context, ExecutionHandle, OutputOptions) (Output, error)
	Artifacts(context.Context, ExecutionHandle, ArtifactOptions) (ArtifactStream, error)
	Cancel(context.Context, ExecutionHandle, CancelOptions) (CancelReceipt, error)
	Delete(context.Context, ExecutionHandle, DeleteOptions) (DeleteReceipt, error)
}

func SameExecution(a, b ExecutionHandle) bool {
	return a.ID() == b.ID() &&
		a.SpecDigest() == b.SpecDigest() &&
		a.BindingDigest() == b.BindingDigest()
}

func ValidateHandle(handle ExecutionHandle) error {
	if handle.ID() == "" {
		return &InvalidError{Field: "handle", Reason: "is empty"}
	}
	if handle.Target().ID == "" {
		return &InvalidError{Field: "handle.target", Reason: "is empty"}
	}
	if handle.SpecDigest().IsZero() || handle.BindingDigest().IsZero() {
		return &InvalidError{Field: "handle.digest", Reason: "is empty"}
	}
	return nil
}

func ReceiptDigest(receipt ExecutionReceipt) (Digest, error) {
	data, err := receipt.MarshalJSON()
	if err != nil {
		return Digest{}, fmt.Errorf("marshal receipt: %w", err)
	}
	return NewDigest(data), nil
}
