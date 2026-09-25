package provider

import (
	"context"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/target"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

type Descriptor struct {
	Name             string
	Priority         int
	ContractVersions []kmx.ContractVersion
	Capabilities     []kmx.Capability
	TargetClasses    []string
	Labels           map[string]string
}

type OpenOptions struct {
	Target      target.Port
	TargetFacts target.Facts
}

type Factory interface {
	Descriptor() Descriptor
	Open(context.Context, OpenOptions) (Backend, error)
}

type Submission struct {
	ExecutionID string
	Spec        kmx.WorkloadSpec
	SpecDigest  kmx.Digest
	Target      target.Facts
}

type InternalReceipt struct {
	Source     string
	Revision   string
	Properties map[string]string
}

type Result struct {
	State           kmx.State
	Message         string
	Events          []kmx.Event
	Logs            []kmx.LogEntry
	Output          kmx.Output
	Artifacts       []kmx.Artifact
	Evidence        []kmx.Evidence
	InternalReceipt InternalReceipt
}

type Backend interface {
	Submit(context.Context, Submission) (Result, error)
	Snapshot(context.Context, string) (Result, error)
	Cancel(context.Context, string, kmx.CancelOptions) (kmx.CancelReceipt, error)
	Delete(context.Context, string, kmx.DeleteOptions) (kmx.DeleteReceipt, error)
	Close() error
}

type ResolutionRequest struct {
	ContractVersion kmx.ContractVersion
	Capabilities    []kmx.Capability
	Constraints     []kmx.Constraint
	TargetClass     string
}
