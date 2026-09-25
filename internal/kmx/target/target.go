package target

import (
	"context"
	"time"

	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

type Facts struct {
	Ref    kmx.TargetRef
	Labels map[string]string
}

type ValidationRequest struct {
	ExecutionID string
	Resources   []Resource
}

type ValidationResult struct {
	Accepted bool
	Warnings []string
}

type Resource struct {
	LogicalName string
	Data        []byte
}

type ApplyRequest struct {
	ExecutionID string
	Resources   []Resource
}

type ApplyReceipt struct {
	ExecutionID string
	Revision    string
	Digest      kmx.Digest
	AppliedAt   time.Time
}

type ReadRequest struct {
	ExecutionID string
}

type Observation struct {
	Found      bool
	Revision   string
	Digest     kmx.Digest
	ObservedAt time.Time
}

type WatchRequest struct {
	ExecutionID string
	After       uint64
}

type ObservationStream interface {
	Next(context.Context) (Observation, error)
	Close() error
}

type DeleteRequest struct {
	ExecutionID string
}

type DeleteReceipt struct {
	ExecutionID string
	Deleted     bool
	DeletedAt   time.Time
}

type Port interface {
	Resolve(context.Context, kmx.TargetSelector) (Facts, error)
	Validate(context.Context, Facts, ValidationRequest) (ValidationResult, error)
	Apply(context.Context, Facts, ApplyRequest) (ApplyReceipt, error)
	Read(context.Context, Facts, ReadRequest) (Observation, error)
	Watch(context.Context, Facts, WatchRequest) (ObservationStream, error)
	Delete(context.Context, Facts, DeleteRequest) (DeleteReceipt, error)
}
