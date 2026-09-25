package conformancetest

import (
	"context"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/provider"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/target"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

func Run(
	t *testing.T,
	factory provider.Factory,
	targetPort target.Port,
	facts target.Facts,
) {
	t.Helper()
	descriptor := factory.Descriptor()
	if descriptor.Name == "" {
		t.Fatal("descriptor name is empty")
	}
	requireCapability(t, descriptor, kmx.CapabilitySubmit)
	requireCapability(t, descriptor, kmx.CapabilityStatus)

	backend, err := factory.Open(context.Background(), provider.OpenOptions{
		Target:      targetPort,
		TargetFacts: facts,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close backend: %v", err)
		}
	})

	spec := kmx.WorkloadSpec{
		APIVersion: kmx.ContractV1Alpha1,
		Kind:       "Workload",
		Name:       "conformance",
		Agent: kmx.AgentSpec{
			Instructions: "run the conformance workload",
			Model:        kmx.ModelRef{Name: "default"},
		},
		Target: kmx.TargetSelector{Class: facts.Ref.Class},
	}
	digest, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const executionID = "conformance-execution"
	result, err := backend.Submit(context.Background(), provider.Submission{
		ExecutionID: executionID,
		Spec:        spec,
		SpecDigest:  digest,
		Target:      facts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State == "" {
		t.Fatal("submit returned an empty state")
	}
	snapshot, err := backend.Snapshot(context.Background(), executionID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != result.State {
		t.Fatalf("snapshot state %q differs from submit state %q", snapshot.State, result.State)
	}
	if hasCapability(descriptor, kmx.CapabilityOutput) && result.Output.Digest.IsZero() {
		t.Fatal("output capability was advertised without output evidence")
	}
	if hasCapability(descriptor, kmx.CapabilityDelete) {
		receipt, err := backend.Delete(context.Background(), executionID, kmx.DeleteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if receipt.ExecutionID != executionID {
			t.Fatalf("delete receipt has execution %q", receipt.ExecutionID)
		}
	}
}

func requireCapability(t *testing.T, descriptor provider.Descriptor, capability kmx.Capability) {
	t.Helper()
	if !hasCapability(descriptor, capability) {
		t.Fatalf("%q does not advertise required capability %q", descriptor.Name, capability)
	}
}

func hasCapability(descriptor provider.Descriptor, capability kmx.Capability) bool {
	for _, advertised := range descriptor.Capabilities {
		if advertised == capability {
			return true
		}
	}
	return false
}
