package interactive

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
		Name:             "interactive",
		Priority:         10,
		ContractVersions: []kmx.ContractVersion{kmx.ContractV1Alpha1},
		Capabilities: []kmx.Capability{
			kmx.CapabilitySubmit,
			kmx.CapabilityStatus,
			kmx.CapabilityEvents,
			kmx.CapabilityLogs,
			kmx.CapabilityOutput,
			kmx.CapabilityCancel,
			kmx.CapabilityDelete,
		},
		TargetClasses: []string{"shared"},
		Labels:        map[string]string{"mode": "conversational"},
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
		Name         string `json:"name"`
		Instructions string `json:"instructions"`
	}{
		Name:         submission.Spec.Name,
		Instructions: submission.Spec.Agent.Instructions,
	})
	if err != nil {
		return provider.Result{}, fmt.Errorf("encode interactive session: %w", err)
	}
	resources := []target.Resource{{LogicalName: "session", Data: data}}
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
	now := time.Now().UTC()
	outputData := []byte("ready:" + submission.Spec.Name)
	result := provider.Result{
		State:   kmx.StateRunning,
		Message: "workload is ready for interaction",
		Events: []kmx.Event{
			{Sequence: 1, Type: "accepted", Time: now},
			{Sequence: 2, Type: "ready", Time: now},
		},
		Logs: []kmx.LogEntry{{
			Sequence: 1,
			Level:    "info",
			Message:  "interaction endpoint is ready",
			Time:     now,
		}},
		Output: kmx.Output{
			MediaType: "text/plain",
			Data:      outputData,
			Digest:    kmx.NewDigest(outputData),
		},
		Evidence: []kmx.Evidence{{
			Name:   "target-application",
			Digest: applied.Digest,
			Time:   applied.AppliedAt,
		}},
		InternalReceipt: provider.InternalReceipt{
			Source:   "interactive",
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
	_ context.Context,
	executionID string,
	_ kmx.CancelOptions,
) (kmx.CancelReceipt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result, ok := b.runs[executionID]
	if !ok {
		return kmx.CancelReceipt{}, &kmx.NotFoundError{Subject: "execution"}
	}
	now := time.Now().UTC()
	result.State = kmx.StateCancelled
	result.Message = "workload was cancelled"
	result.Events = append(result.Events, kmx.Event{
		Sequence: uint64(len(result.Events) + 1),
		Type:     "cancelled",
		Time:     now,
	})
	b.runs[executionID] = result
	return kmx.CancelReceipt{ExecutionID: executionID, Accepted: true, Time: now}, nil
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
