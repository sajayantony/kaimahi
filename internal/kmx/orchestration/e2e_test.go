package orchestration_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/implementations/batch"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/implementations/interactive"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/orchestration"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/provider"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/target/memory"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

func TestPortableWorkloadEndToEnd(t *testing.T) {
	t.Parallel()
	service, targetPort := newService(t)
	ctx := context.Background()
	spec := workload(kmx.CapabilityArtifacts)

	handle, err := service.Submit(ctx, spec, kmx.SubmitOptions{IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := service.Submit(ctx, spec, kmx.SubmitOptions{IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !kmx.SameExecution(handle, same) {
		t.Fatal("same idempotent request returned a different execution")
	}

	snapshot, err := service.Get(ctx, handle, kmx.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != kmx.StateSucceeded || snapshot.Receipt == nil {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	events, err := service.Watch(ctx, handle, kmx.WatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	event, err := events.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "accepted" {
		t.Fatalf("unexpected event: %#v", event)
	}
	output, err := service.Output(ctx, handle, kmx.OutputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(output.Data) != "completed:portable-example" {
		t.Fatalf("unexpected output %q", output.Data)
	}
	artifacts, err := service.Artifacts(ctx, handle, kmx.ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := artifacts.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Name != "execution-evidence.txt" {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}
	if _, err := artifacts.Next(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("expected end of artifacts, got %v", err)
	}
	deleted, err := service.Delete(ctx, handle, kmx.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Deleted {
		t.Fatal("workload resources were not deleted")
	}
	journal := targetPort.Journal()
	for _, operation := range []string{
		"resolve",
		"validate:" + handle.ID(),
		"apply:" + handle.ID(),
		"read:" + handle.ID(),
		"delete:" + handle.ID(),
	} {
		if !slices.Contains(journal, operation) {
			t.Fatalf("target journal %v does not contain %q", journal, operation)
		}
	}
}

func TestDistinctImplementationAndTypedUnsupportedOperation(t *testing.T) {
	t.Parallel()
	service, _ := newService(t)
	ctx := context.Background()
	spec := workload(kmx.CapabilityLogs)
	spec.Requirements.Constraints = []kmx.Constraint{{
		Name:     "mode",
		Operator: "equals",
		Values:   []string{"conversational"},
	}}
	handle, err := service.Submit(ctx, spec, kmx.SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	logs, err := service.Logs(ctx, handle, kmx.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logs.Next(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = service.Artifacts(ctx, handle, kmx.ArtifactOptions{})
	if !kmx.IsCode(err, kmx.CodeUnsupported) {
		t.Fatalf("expected typed unsupported error, got %v", err)
	}
	cancel, err := service.Cancel(ctx, handle, kmx.CancelOptions{Reason: "test complete"})
	if err != nil {
		t.Fatal(err)
	}
	if !cancel.Accepted {
		t.Fatal("cancel was not accepted")
	}
}

func TestIdempotencyConflict(t *testing.T) {
	t.Parallel()
	service, _ := newService(t)
	ctx := context.Background()
	first := workload(kmx.CapabilityArtifacts)
	if _, err := service.Submit(ctx, first, kmx.SubmitOptions{IdempotencyKey: "same"}); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Name = "changed"
	_, err := service.Submit(ctx, second, kmx.SubmitOptions{IdempotencyKey: "same"})
	if !kmx.IsCode(err, kmx.CodeConflict) {
		t.Fatalf("expected typed conflict, got %v", err)
	}
}

func TestAdditionalImplementationNeedsNoServiceOrContractChange(t *testing.T) {
	t.Parallel()
	registry := provider.NewRegistry()
	if err := registry.Register(batch.Factory{}); err != nil {
		t.Fatal(err)
	}
	const extra kmx.Capability = "specialized-output"
	if err := registry.Register(specializedFactory{capability: extra}); err != nil {
		t.Fatal(err)
	}
	targetPort := memory.New(kmx.TargetRef{ID: "target-1", Class: "shared"}, nil)
	service, err := orchestration.NewService(registry, targetPort)
	if err != nil {
		t.Fatal(err)
	}
	spec := workload(extra)
	handle, err := service.Submit(context.Background(), spec, kmx.SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	output, err := service.Output(context.Background(), handle, kmx.OutputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(output.Data) != "specialized" {
		t.Fatalf("new implementation was not selected: %q", output.Data)
	}
}

func newService(t *testing.T) (*orchestration.Service, *memory.Target) {
	t.Helper()
	registry := provider.NewRegistry()
	for _, factory := range []provider.Factory{batch.Factory{}, interactive.Factory{}} {
		if err := registry.Register(factory); err != nil {
			t.Fatal(err)
		}
	}
	targetPort := memory.New(
		kmx.TargetRef{ID: "target-1", Class: "shared", Scope: "team-a"},
		map[string]string{"region": "test"},
	)
	service, err := orchestration.NewService(registry, targetPort)
	if err != nil {
		t.Fatal(err)
	}
	return service, targetPort
}

func workload(capabilities ...kmx.Capability) kmx.WorkloadSpec {
	return kmx.WorkloadSpec{
		APIVersion: kmx.ContractV1Alpha1,
		Kind:       "Workload",
		Name:       "portable-example",
		Agent: kmx.AgentSpec{
			Instructions: "produce a deterministic result",
			Model:        kmx.ModelRef{Name: "default"},
		},
		Requirements: kmx.RequirementSet{Capabilities: capabilities},
		Target:       kmx.TargetSelector{Class: "shared"},
		Inputs:       map[string]string{"request": "demo"},
	}
}

type specializedFactory struct {
	capability kmx.Capability
}

func (f specializedFactory) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		Name:             "specialized",
		Priority:         100,
		ContractVersions: []kmx.ContractVersion{kmx.ContractV1Alpha1},
		Capabilities: []kmx.Capability{
			kmx.CapabilitySubmit,
			kmx.CapabilityStatus,
			kmx.CapabilityOutput,
			f.capability,
		},
		TargetClasses: []string{"shared"},
	}
}

func (specializedFactory) Open(context.Context, provider.OpenOptions) (provider.Backend, error) {
	return specializedBackend{}, nil
}

type specializedBackend struct{}

func (specializedBackend) Submit(
	_ context.Context,
	submission provider.Submission,
) (provider.Result, error) {
	data := []byte("specialized")
	return provider.Result{
		State: kmx.StateSucceeded,
		Output: kmx.Output{
			MediaType: "text/plain",
			Data:      data,
			Digest:    kmx.NewDigest(data),
		},
		Events: []kmx.Event{{Sequence: 1, Type: "completed"}},
	}, nil
}

func (specializedBackend) Snapshot(context.Context, string) (provider.Result, error) {
	data := []byte("specialized")
	return provider.Result{
		State:  kmx.StateSucceeded,
		Output: kmx.Output{MediaType: "text/plain", Data: data, Digest: kmx.NewDigest(data)},
	}, nil
}

func (specializedBackend) Cancel(
	context.Context,
	string,
	kmx.CancelOptions,
) (kmx.CancelReceipt, error) {
	return kmx.CancelReceipt{}, &kmx.UnsupportedError{Operation: "cancel"}
}

func (specializedBackend) Delete(
	context.Context,
	string,
	kmx.DeleteOptions,
) (kmx.DeleteReceipt, error) {
	return kmx.DeleteReceipt{}, &kmx.UnsupportedError{Operation: "delete"}
}

func (specializedBackend) Close() error { return nil }
