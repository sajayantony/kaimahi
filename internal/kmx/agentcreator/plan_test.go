package agentcreator

import (
	"strings"
	"testing"
)

func validIntent() AgentIntent {
	return AgentIntent{
		APIVersion: IntentAPIVersion,
		Kind:       IntentKind,
		Metadata:   IntentMetadata{Name: "reviewer"},
		Spec: IntentSpec{
			Description:  "Reviews changes",
			Instructions: "Review changes and state uncertainty plainly.",
			Namespace:    "agents",
			Provider: ProviderIntent{
				Type: "openai", Model: "gpt-5",
				SecretRef: SecretRefIntent{Name: "model-key"},
			},
			Execution:  ExecutionIntent{Sandbox: "auto", Language: "javascript"},
			Deployment: DeploymentIntent{Mode: "offline", Output: "reviewer.yaml", BundlePath: "reviewer"},
		},
	}
}

func TestBuildPlanNormalizesAndSelectsSandbox(t *testing.T) {
	result, err := BuildPlan(validIntent())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "review_required" || result.Plan == nil {
		t.Fatal(result)
	}
	if got := string(result.Plan.Sandbox.Backend); got != "hyperlight-js" {
		t.Fatalf("sandbox=%q", got)
	}
	if !strings.HasPrefix(result.Plan.Digest, "sha256:") {
		t.Fatal(result.Plan.Digest)
	}
}

func TestBuildPlanReturnsQuestionsInsteadOfGuessing(t *testing.T) {
	result, err := BuildPlan(AgentIntent{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "needs_input" || result.Plan != nil || len(result.Questions) < 5 {
		t.Fatal(result)
	}
}

func TestApplyRequiresExactReviewedDigest(t *testing.T) {
	result, err := BuildPlan(validIntent())
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyRequest{Plan: *result.Plan, ApprovedDigest: result.Plan.Digest}
	if err := VerifyApplyRequest(request); err != nil {
		t.Fatal(err)
	}
	request.Plan.Intent.Spec.Instructions = "Changed after review"
	if err := VerifyApplyRequest(request); err == nil {
		t.Fatal("accepted a changed plan")
	}
	request = ApplyRequest{Plan: *result.Plan, ApprovedDigest: "sha256:wrong"}
	if err := VerifyApplyRequest(request); err == nil {
		t.Fatal("accepted the wrong approval digest")
	}
}
