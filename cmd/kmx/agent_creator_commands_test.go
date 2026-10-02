package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentcreator"
)

func creatorIntent(dir string) agentcreator.AgentIntent {
	return agentcreator.AgentIntent{
		APIVersion: agentcreator.IntentAPIVersion,
		Kind:       agentcreator.IntentKind,
		Metadata:   agentcreator.IntentMetadata{Name: "reviewer"},
		Spec: agentcreator.IntentSpec{
			Description:  "Reviews changes",
			Instructions: "Review changes carefully.",
			Namespace:    "agents",
			Provider: agentcreator.ProviderIntent{
				Type: "openai", Model: "gpt-5",
				SecretRef: agentcreator.SecretRefIntent{Name: "model-key"},
			},
			Execution: agentcreator.ExecutionIntent{Sandbox: "auto", Language: "javascript"},
			Deployment: agentcreator.DeploymentIntent{
				Mode: "offline", Output: filepath.Join(dir, "reviewer.yaml"), BundlePath: filepath.Join(dir, "reviewer"),
			},
		},
	}
}

func TestAgentCreatorPlanDoesNotLoadKubeConfiguration(t *testing.T) {
	dir := t.TempDir()
	intentPath := filepath.Join(dir, "intent.json")
	writeJSONFile(t, intentPath, creatorIntent(dir))
	var out, diagnostics bytes.Buffer
	deps, loads := testDependencies(&out, &diagnostics)
	if err := execute([]string{"agent", "creator", "plan", "--intent", intentPath}, deps); err != nil {
		t.Fatal(err)
	}
	if *loads != 0 {
		t.Fatalf("plan loaded kube configuration %d times", *loads)
	}
	var result agentcreator.PlanResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || result.Plan.Sandbox == nil {
		t.Fatal(result)
	}
}

func TestAgentCreatorApplyRequiresReviewedDigest(t *testing.T) {
	dir := t.TempDir()
	planned, err := agentcreator.BuildPlan(creatorIntent(dir))
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan.json")
	writeJSONFile(t, planPath, planned)

	var out, diagnostics bytes.Buffer
	deps, _ := testDependencies(&out, &diagnostics)
	if err := execute([]string{"agent", "creator", "apply", "--plan", planPath, "--approve", "sha256:wrong"}, deps); err == nil {
		t.Fatal("wrong digest was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "reviewer.yaml")); !os.IsNotExist(err) {
		t.Fatalf("invalid approval wrote an artifact: %v", err)
	}

	out.Reset()
	diagnostics.Reset()
	deps, _ = testDependencies(&out, &diagnostics)
	if err := execute([]string{"agent", "creator", "apply", "--plan", planPath, "--approve", planned.Plan.Digest}, deps); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reviewer.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reviewer", "agent.yaml")); err != nil {
		t.Fatal(err)
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
