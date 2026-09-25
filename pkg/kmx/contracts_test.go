package kmx_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

func TestExecutionHandleRoundTrip(t *testing.T) {
	t.Parallel()
	handle, err := kmx.NewExecutionHandle(
		"exec-1",
		kmx.TargetRef{ID: "target-1", Class: "shared", Scope: "team-a"},
		kmx.NewDigest([]byte("spec")),
		kmx.NewDigest([]byte("binding")),
		kmx.ContractV1Alpha1,
		time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(handle)
	if err != nil {
		t.Fatal(err)
	}
	var decoded kmx.ExecutionHandle
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !kmx.SameExecution(handle, decoded) {
		t.Fatalf("round trip changed handle: %#v", decoded)
	}
}

func TestWorkloadValidation(t *testing.T) {
	t.Parallel()
	spec := validSpec()
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.Requirements.Constraints = []kmx.Constraint{{
		Name:     "mode",
		Operator: "equals",
		Values:   []string{"finite", "other"},
	}}
	if err := spec.Validate(); !kmx.IsCode(err, kmx.CodeInvalid) {
		t.Fatalf("expected typed invalid error, got %v", err)
	}

	spec = validSpec()
	spec.Requirements.Constraints = []kmx.Constraint{{
		Name:     "provider",
		Operator: "equals",
		Values:   []string{"named-choice"},
	}}
	if err := spec.Validate(); !kmx.IsCode(err, kmx.CodeInvalid) {
		t.Fatalf("expected implementation-selection-shaped constraint to be rejected, got %v", err)
	}
}

func validSpec() kmx.WorkloadSpec {
	return kmx.WorkloadSpec{
		APIVersion: kmx.ContractV1Alpha1,
		Kind:       "Workload",
		Name:       "example",
		Agent: kmx.AgentSpec{
			Instructions: "complete the requested task",
			Model:        kmx.ModelRef{Name: "default"},
		},
		Target: kmx.TargetSelector{Class: "shared"},
	}
}
