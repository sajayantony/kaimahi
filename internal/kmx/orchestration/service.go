package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/provider"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/target"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

type Service struct {
	registry *provider.Registry
	target   target.Port
	now      func() time.Time
	nextID   atomic.Uint64

	mu          sync.Mutex
	executions  map[string]*execution
	idempotency map[string]idempotentSubmission
}

type execution struct {
	handle      kmx.ExecutionHandle
	descriptor  provider.Descriptor
	backend     provider.Backend
	result      provider.Result
	receipt     *kmx.ExecutionReceipt
	targetFacts target.Facts
}

type idempotentSubmission struct {
	specDigest kmx.Digest
	handle     kmx.ExecutionHandle
}

func NewService(registry *provider.Registry, targetPort target.Port) (*Service, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if targetPort == nil {
		return nil, fmt.Errorf("target port is required")
	}
	return &Service{
		registry:    registry,
		target:      targetPort,
		now:         time.Now,
		executions:  make(map[string]*execution),
		idempotency: make(map[string]idempotentSubmission),
	}, nil
}

func (s *Service) Submit(
	ctx context.Context,
	spec kmx.WorkloadSpec,
	options kmx.SubmitOptions,
) (kmx.ExecutionHandle, error) {
	if err := spec.Validate(); err != nil {
		return kmx.ExecutionHandle{}, err
	}
	specDigest, err := spec.Digest()
	if err != nil {
		return kmx.ExecutionHandle{}, err
	}
	key := options.IdempotencyKey
	if key == "" {
		key = specDigest.Value
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, exists := s.idempotency[key]; exists {
		if previous.specDigest != specDigest {
			return kmx.ExecutionHandle{}, &kmx.ConflictError{
				Field:  "idempotencyKey",
				Reason: "was already used with a different workload digest",
			}
		}
		return previous.handle, nil
	}

	targetFacts, err := s.target.Resolve(ctx, spec.Target)
	if err != nil {
		return kmx.ExecutionHandle{}, err
	}
	factory, err := s.registry.Resolve(provider.ResolutionRequest{
		ContractVersion: spec.APIVersion,
		Capabilities:    append([]kmx.Capability{kmx.CapabilitySubmit, kmx.CapabilityStatus}, spec.Requirements.Capabilities...),
		Constraints:     spec.Requirements.Constraints,
		TargetClass:     targetFacts.Ref.Class,
	})
	if err != nil {
		return kmx.ExecutionHandle{}, err
	}
	backend, err := s.registry.Open(ctx, factory, provider.OpenOptions{
		Target:      s.target,
		TargetFacts: targetFacts,
	})
	if err != nil {
		return kmx.ExecutionHandle{}, err
	}

	executionID := fmt.Sprintf("exec-%06d", s.nextID.Add(1))
	bindingData, err := json.Marshal(struct {
		Target string `json:"target"`
		Choice string `json:"choice"`
	}{
		Target: targetFacts.Ref.ID,
		Choice: factory.Descriptor().Name,
	})
	if err != nil {
		_ = backend.Close()
		return kmx.ExecutionHandle{}, fmt.Errorf("marshal binding identity: %w", err)
	}
	handle, err := kmx.NewExecutionHandle(
		executionID,
		targetFacts.Ref,
		specDigest,
		kmx.NewDigest(bindingData),
		spec.APIVersion,
		s.now().UTC(),
	)
	if err != nil {
		_ = backend.Close()
		return kmx.ExecutionHandle{}, err
	}
	result, err := backend.Submit(ctx, provider.Submission{
		ExecutionID: executionID,
		Spec:        spec,
		SpecDigest:  specDigest,
		Target:      targetFacts,
	})
	if err != nil {
		_ = backend.Close()
		return kmx.ExecutionHandle{}, err
	}
	record := &execution{
		handle:      handle,
		descriptor:  factory.Descriptor(),
		backend:     backend,
		result:      result,
		targetFacts: targetFacts,
	}
	if result.State.IsFinal() {
		receipt, receiptErr := makeReceipt(handle, result, s.now().UTC())
		if receiptErr != nil {
			_ = backend.Close()
			return kmx.ExecutionHandle{}, receiptErr
		}
		record.receipt = &receipt
	}
	s.executions[executionID] = record
	s.idempotency[key] = idempotentSubmission{specDigest: specDigest, handle: handle}
	return handle, nil
}

func (s *Service) Get(
	ctx context.Context,
	handle kmx.ExecutionHandle,
	_ kmx.GetOptions,
) (kmx.ExecutionSnapshot, error) {
	record, err := s.lookup(handle)
	if err != nil {
		return kmx.ExecutionSnapshot{}, err
	}
	result, err := record.backend.Snapshot(ctx, handle.ID())
	if err != nil {
		return kmx.ExecutionSnapshot{}, err
	}
	s.mu.Lock()
	record.result = result
	if result.State.IsFinal() && record.receipt == nil {
		receipt, receiptErr := makeReceipt(handle, result, s.now().UTC())
		if receiptErr != nil {
			s.mu.Unlock()
			return kmx.ExecutionSnapshot{}, receiptErr
		}
		record.receipt = &receipt
	}
	var receipt *kmx.ExecutionReceipt
	if record.receipt != nil {
		value := *record.receipt
		receipt = &value
	}
	s.mu.Unlock()
	return kmx.ExecutionSnapshot{
		Handle:          handle,
		State:           result.State,
		Sequence:        uint64(len(result.Events)),
		UpdatedAt:       s.now().UTC(),
		Message:         result.Message,
		ResultAvailable: !result.Output.Digest.IsZero(),
		Receipt:         receipt,
	}, nil
}

func (s *Service) Watch(
	_ context.Context,
	handle kmx.ExecutionHandle,
	options kmx.WatchOptions,
) (kmx.EventStream, error) {
	record, err := s.lookup(handle)
	if err != nil {
		return nil, err
	}
	if !hasCapability(record.descriptor, kmx.CapabilityEvents) {
		return nil, unsupported("watch", kmx.CapabilityEvents)
	}
	values := make([]kmx.Event, 0, len(record.result.Events))
	for _, event := range record.result.Events {
		if event.Sequence > options.AfterSequence {
			values = append(values, event)
		}
	}
	return &sliceStream[kmx.Event]{values: values}, nil
}

func (s *Service) Logs(
	_ context.Context,
	handle kmx.ExecutionHandle,
	options kmx.LogOptions,
) (kmx.LogStream, error) {
	record, err := s.lookup(handle)
	if err != nil {
		return nil, err
	}
	if !hasCapability(record.descriptor, kmx.CapabilityLogs) {
		return nil, unsupported("logs", kmx.CapabilityLogs)
	}
	values := make([]kmx.LogEntry, 0, len(record.result.Logs))
	for _, entry := range record.result.Logs {
		if entry.Sequence > options.AfterSequence {
			values = append(values, entry)
		}
	}
	return &sliceStream[kmx.LogEntry]{values: values}, nil
}

func (s *Service) Output(
	_ context.Context,
	handle kmx.ExecutionHandle,
	_ kmx.OutputOptions,
) (kmx.Output, error) {
	record, err := s.lookup(handle)
	if err != nil {
		return kmx.Output{}, err
	}
	if !hasCapability(record.descriptor, kmx.CapabilityOutput) {
		return kmx.Output{}, unsupported("output", kmx.CapabilityOutput)
	}
	return cloneOutput(record.result.Output), nil
}

func (s *Service) Artifacts(
	_ context.Context,
	handle kmx.ExecutionHandle,
	_ kmx.ArtifactOptions,
) (kmx.ArtifactStream, error) {
	record, err := s.lookup(handle)
	if err != nil {
		return nil, err
	}
	if !hasCapability(record.descriptor, kmx.CapabilityArtifacts) {
		return nil, unsupported("artifacts", kmx.CapabilityArtifacts)
	}
	values := make([]kmx.Artifact, len(record.result.Artifacts))
	for i, artifact := range record.result.Artifacts {
		values[i] = cloneArtifact(artifact)
	}
	return &sliceStream[kmx.Artifact]{values: values}, nil
}

func (s *Service) Cancel(
	ctx context.Context,
	handle kmx.ExecutionHandle,
	options kmx.CancelOptions,
) (kmx.CancelReceipt, error) {
	record, err := s.lookup(handle)
	if err != nil {
		return kmx.CancelReceipt{}, err
	}
	if !hasCapability(record.descriptor, kmx.CapabilityCancel) {
		return kmx.CancelReceipt{}, unsupported("cancel", kmx.CapabilityCancel)
	}
	return record.backend.Cancel(ctx, handle.ID(), options)
}

func (s *Service) Delete(
	ctx context.Context,
	handle kmx.ExecutionHandle,
	options kmx.DeleteOptions,
) (kmx.DeleteReceipt, error) {
	record, err := s.lookup(handle)
	if err != nil {
		return kmx.DeleteReceipt{}, err
	}
	if !hasCapability(record.descriptor, kmx.CapabilityDelete) {
		return kmx.DeleteReceipt{}, unsupported("delete", kmx.CapabilityDelete)
	}
	receipt, err := record.backend.Delete(ctx, handle.ID(), options)
	if err != nil {
		return kmx.DeleteReceipt{}, err
	}
	if err := record.backend.Close(); err != nil {
		return kmx.DeleteReceipt{}, fmt.Errorf("close execution: %w", err)
	}
	s.mu.Lock()
	delete(s.executions, handle.ID())
	s.mu.Unlock()
	return receipt, nil
}

func (s *Service) lookup(handle kmx.ExecutionHandle) (*execution, error) {
	if err := kmx.ValidateHandle(handle); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.executions[handle.ID()]
	if !exists || !kmx.SameExecution(record.handle, handle) {
		return nil, &kmx.NotFoundError{Subject: "execution"}
	}
	return record, nil
}

func makeReceipt(
	handle kmx.ExecutionHandle,
	result provider.Result,
	completedAt time.Time,
) (kmx.ExecutionReceipt, error) {
	resultData, err := json.Marshal(struct {
		State     kmx.State      `json:"state"`
		Output    kmx.Digest     `json:"output"`
		Artifacts []kmx.Artifact `json:"artifacts,omitempty"`
	}{
		State:     result.State,
		Output:    result.Output.Digest,
		Artifacts: result.Artifacts,
	})
	if err != nil {
		return kmx.ExecutionReceipt{}, fmt.Errorf("marshal execution result: %w", err)
	}
	return kmx.NewExecutionReceipt(
		handle.ID(),
		handle.SpecDigest(),
		handle.BindingDigest(),
		kmx.NewDigest(resultData),
		result.Evidence,
		completedAt,
	)
}

func hasCapability(descriptor provider.Descriptor, wanted kmx.Capability) bool {
	for _, capability := range descriptor.Capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}

func unsupported(operation string, capability kmx.Capability) error {
	return &kmx.UnsupportedError{Operation: operation, Capability: capability}
}

func cloneOutput(value kmx.Output) kmx.Output {
	value.Data = append([]byte(nil), value.Data...)
	return value
}

func cloneArtifact(value kmx.Artifact) kmx.Artifact {
	value.Data = append([]byte(nil), value.Data...)
	return value
}
