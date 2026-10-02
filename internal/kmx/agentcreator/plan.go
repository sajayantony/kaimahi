package agentcreator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

func BuildPlan(input AgentIntent) (PlanResult, error) {
	intent := input
	if intent.APIVersion == "" {
		intent.APIVersion = IntentAPIVersion
	}
	if intent.Kind == "" {
		intent.Kind = IntentKind
	}
	if intent.APIVersion != IntentAPIVersion || intent.Kind != IntentKind {
		return PlanResult{}, fmt.Errorf("agent intent must be %s %s", IntentAPIVersion, IntentKind)
	}
	if err := refuseCredentialShapes(intent); err != nil {
		return PlanResult{}, err
	}
	intent.Spec.Runtime = strings.TrimSpace(intent.Spec.Runtime)
	if intent.Spec.Runtime == "" {
		intent.Spec.Runtime = "orka"
	}
	if intent.Spec.Runtime != "orka" && intent.Spec.Runtime != "kagent" {
		return PlanResult{}, fmt.Errorf("spec.runtime must be orka or kagent")
	}
	if intent.Spec.KagentRuntime == "" {
		intent.Spec.KagentRuntime = "go"
	}
	if intent.Spec.Runtime == "kagent" && intent.Spec.KagentRuntime != "go" && intent.Spec.KagentRuntime != "python" {
		return PlanResult{}, fmt.Errorf("spec.kagentRuntime must be go or python")
	}
	if intent.Spec.Provider.SecretRef.Key == "" {
		intent.Spec.Provider.SecretRef.Key = scaffold.DefaultOrkaSecretKey
	}
	if intent.Spec.Deployment.Mode == "" {
		intent.Spec.Deployment.Mode = "offline"
	}
	if intent.Spec.Deployment.Mode != "offline" && intent.Spec.Deployment.Mode != "cluster" {
		return PlanResult{}, fmt.Errorf("spec.deployment.mode must be offline or cluster")
	}

	questions := missingQuestions(intent)
	if len(questions) > 0 {
		return PlanResult{Status: "needs_input", Questions: questions}, nil
	}
	if err := scaffold.ValidateName(intent.Metadata.Name); err != nil {
		return PlanResult{}, fmt.Errorf("metadata.name: %w", err)
	}
	if intent.Spec.Deployment.Output == "" {
		intent.Spec.Deployment.Output = filepath.Join("agents", intent.Metadata.Name+".yaml")
	}
	if intent.Spec.Deployment.BundlePath == "" {
		intent.Spec.Deployment.BundlePath = filepath.Join("agents", intent.Metadata.Name)
	}
	if intent.Spec.Runtime == "kagent" && intent.Spec.Deployment.Mode == "offline" && intent.Spec.Task != "" {
		return PlanResult{}, fmt.Errorf("Kagent offline creation cannot include spec.task")
	}

	sandboxPlan, err := agentruntime.SelectSandbox(intent.Spec.Execution.Sandbox, agentruntime.SandboxRequirements{
		Language:       intent.Spec.Execution.Language,
		Shell:          intent.Spec.Execution.Shell,
		NativePackages: intent.Spec.Execution.NativePackages,
		ContainerImage: intent.Spec.Execution.ContainerImage,
		DeviceAccess:   intent.Spec.Execution.DeviceAccess,
	})
	if err != nil {
		return PlanResult{}, err
	}
	var sandbox *agentruntime.SandboxSpec
	if sandboxPlan != nil {
		spec := sandboxPlan.Spec
		sandbox = &spec
		intent.Spec.Execution.Language = spec.Requirements.Language
	}

	plan := AgentCreationPlan{
		APIVersion: IntentAPIVersion,
		Kind:       PlanKind,
		Intent:     intent,
		Runtime:    intent.Spec.Runtime,
		Sandbox:    sandbox,
		Mutations:  plannedMutations(intent),
	}
	digest, err := planDigest(plan)
	if err != nil {
		return PlanResult{}, err
	}
	plan.Digest = digest
	return PlanResult{Status: "review_required", Plan: &plan}, nil
}

func VerifyApplyRequest(request ApplyRequest) error {
	if request.ApprovedDigest == "" {
		return fmt.Errorf("approvedDigest is required and must be copied from the reviewed plan")
	}
	if request.ApprovedDigest != request.Plan.Digest {
		return fmt.Errorf("approvedDigest does not match plan.digest")
	}
	expected, err := planDigest(request.Plan)
	if err != nil {
		return err
	}
	if expected != request.Plan.Digest {
		return fmt.Errorf("plan content changed after its digest was computed")
	}
	result, err := BuildPlan(request.Plan.Intent)
	if err != nil {
		return err
	}
	if result.Plan == nil || result.Plan.Digest != request.Plan.Digest {
		return fmt.Errorf("plan no longer matches normalized intent; create and review a new plan")
	}
	return nil
}

func planDigest(plan AgentCreationPlan) (string, error) {
	plan.Digest = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode agent creation plan: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func missingQuestions(intent AgentIntent) []Question {
	var questions []Question
	add := func(id, prompt string, choices ...string) {
		questions = append(questions, Question{ID: id, Prompt: prompt, Choices: choices, Required: true})
	}
	if strings.TrimSpace(intent.Metadata.Name) == "" {
		add("name", "What Kubernetes-compatible name should this agent use?")
	}
	if strings.TrimSpace(intent.Spec.Namespace) == "" {
		add("namespace", "Which explicit namespace should contain the agent?")
	}
	if strings.TrimSpace(intent.Spec.Provider.Type) == "" {
		add("provider-type", "Which model provider should the agent use?", "openai", "anthropic", "azure-openai")
	}
	if strings.TrimSpace(intent.Spec.Provider.Model) == "" {
		add("model", "Which provider model identifier should the agent use?")
	}
	if strings.TrimSpace(intent.Spec.Provider.SecretRef.Name) == "" {
		add("secret-ref", "Which existing Kubernetes Secret should the provider reference?")
	}
	if intent.Spec.Runtime == "kagent" && strings.TrimSpace(intent.Spec.Description) == "" {
		add("description", "What one-line description should the Kagent Agent carry?")
	}
	return questions
}

func plannedMutations(intent AgentIntent) []Mutation {
	if intent.Spec.Deployment.Mode == "offline" {
		return []Mutation{
			{Action: "write", Kind: "PortableAgent", Name: intent.Spec.Deployment.BundlePath},
			{Action: "write", Kind: "ReviewArtifact", Name: intent.Spec.Deployment.Output},
		}
	}
	providerKind := "Provider"
	if intent.Spec.Runtime == "kagent" {
		providerKind = "ModelConfig"
	}
	mutations := []Mutation{
		{Action: "create", Kind: providerKind, Name: intent.Metadata.Name, Namespace: intent.Spec.Namespace},
		{Action: "create", Kind: "Agent", Name: intent.Metadata.Name, Namespace: intent.Spec.Namespace},
	}
	if intent.Spec.Task != "" {
		mutations = append(mutations, Mutation{Action: "create", Kind: "Task", Name: "<generated>", Namespace: intent.Spec.Namespace})
	}
	return mutations
}

func refuseCredentialShapes(intent AgentIntent) error {
	values := []string{
		intent.Metadata.Name, intent.Spec.Description, intent.Spec.Instructions, intent.Spec.Runtime,
		intent.Spec.KagentRuntime, intent.Spec.Namespace, intent.Spec.Provider.Type, intent.Spec.Provider.Model,
		intent.Spec.Provider.BaseURL, intent.Spec.Provider.SecretRef.Name, intent.Spec.Provider.SecretRef.Key,
		intent.Spec.Task, intent.Spec.Execution.Sandbox, intent.Spec.Execution.Language,
		intent.Spec.Deployment.Mode, intent.Spec.Deployment.Output, intent.Spec.Deployment.BundlePath,
	}
	values = append(values, intent.Spec.Tools...)
	values = append(values, intent.Spec.Skills...)
	for _, value := range values {
		if err := scaffold.RefuseKeyShapes(value); err != nil {
			return fmt.Errorf("agent intent contains something credential-shaped; use Secret references, never values")
		}
	}
	return nil
}
