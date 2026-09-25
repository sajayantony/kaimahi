package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/provider"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/target"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

type Factory struct{}

func (Factory) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		Name:             "batch",
		Priority:         20,
		ContractVersions: []kmx.ContractVersion{kmx.ContractV1Alpha1},
		Capabilities: []kmx.Capability{
			kmx.CapabilitySubmit,
			kmx.CapabilityStatus,
			kmx.CapabilityEvents,
			kmx.CapabilityOutput,
			kmx.CapabilityArtifacts,
			kmx.CapabilityDelete,
		},
		TargetClasses: []string{"shared"},
		Labels:        map[string]string{"mode": "finite"},
	}
}

func (Factory) Open(_ context.Context, options provider.OpenOptions) (provider.Backend, error) {
	if options.Target == nil {
		return nil, fmt.Errorf("target port is required")
	}
	return &backend{
		target: options.Target,
		facts:  options.TargetFacts,
		runs:   make(map[string]provider.Result),
	}, nil
}

type backend struct {
	mu     sync.Mutex
	target target.Port
	facts  target.Facts
	runs   map[string]provider.Result
}

func (b *backend) Submit(ctx context.Context, submission provider.Submission) (provider.Result, error) {
	data, err := json.Marshal(struct {
		Name   string            `json:"name"`
		Inputs map[string]string `json:"inputs,omitempty"`
	}{
		Name:   submission.Spec.Name,
		Inputs: submission.Spec.Inputs,
	})
	if err != nil {
		return provider.Result{}, fmt.Errorf("encode batch unit: %w", err)
	}
	resources := []target.Resource{{LogicalName: "work-unit", Data: data}}
	if _, err := b.target.Validate(ctx, b.facts, target.ValidationRequest{
		ExecutionID: submission.ExecutionID,
		Resources:   resources,
	}); err != nil {
		return provider.Result{}, err
	}
	applied, err := b.target.Apply(ctx, b.facts, target.ApplyRequest{
		ExecutionID: submission.ExecutionID,
		Resources:   resources,
	})
	if err != nil {
		return provider.Result{}, err
	}
	observation, err := b.target.Read(ctx, b.facts, target.ReadRequest{ExecutionID: submission.ExecutionID})
	if err != nil {
		return provider.Result{}, err
	}
	if !observation.Found {
		return provider.Result{}, &kmx.OutcomeUnknownError{
			Operation: "submit",
			Reason:    "target did not confirm the applied workload",
		}
	}
	now := time.Now().UTC()
	outputData := []byte("completed:" + submission.Spec.Name)
	artifactData := []byte("evidence:" + submission.ExecutionID)
	result := provider.Result{
		State:   kmx.StateSucceeded,
		Message: "workload completed",
		Events: []kmx.Event{
			{Sequence: 1, Type: "accepted", Time: now},
			{Sequence: 2, Type: "completed", Time: now},
		},
		Output: kmx.Output{
			MediaType: "text/plain",
			Data:      outputData,
			Digest:    kmx.NewDigest(outputData),
		},
		Artifacts: []kmx.Artifact{{
			Name:      "execution-evidence.txt",
			MediaType: "text/plain",
			Data:      artifactData,
			Digest:    kmx.NewDigest(artifactData),
		}},
		Evidence: []kmx.Evidence{{
			Name:   "target-application",
			Digest: applied.Digest,
			Time:   applied.AppliedAt,
		}},
		InternalReceipt: provider.InternalReceipt{
			Source:   "batch",
			Revision: applied.Revision,
		},
	}
	b.mu.Lock()
	b.runs[submission.ExecutionID] = result
	b.mu.Unlock()
	return result, nil
}

func (b *backend) Snapshot(_ context.Context, executionID string) (provider.Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result, ok := b.runs[executionID]
	if !ok {
		return provider.Result{}, &kmx.NotFoundError{Subject: "execution"}
	}
	return result, nil
}

func (b *backend) Cancel(
	context.Context,
	string,
	kmx.CancelOptions,
) (kmx.CancelReceipt, error) {
	return kmx.CancelReceipt{}, &kmx.UnsupportedError{
		Operation:  "cancel",
		Capability: kmx.CapabilityCancel,
	}
}

func (b *backend) Delete(
	ctx context.Context,
	executionID string,
	_ kmx.DeleteOptions,
) (kmx.DeleteReceipt, error) {
	receipt, err := b.target.Delete(ctx, b.facts, target.DeleteRequest{ExecutionID: executionID})
	if err != nil {
		return kmx.DeleteReceipt{}, err
	}
	b.mu.Lock()
	delete(b.runs, executionID)
	b.mu.Unlock()
	return kmx.DeleteReceipt{
		ExecutionID: executionID,
		Deleted:     receipt.Deleted,
		Time:        receipt.DeletedAt,
	}, nil
}

func (*backend) Close() error { return nil }
