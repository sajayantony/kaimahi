package agentcreator

import agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"

const (
	IntentAPIVersion = "kmx.kaimahi.dev/v1alpha1"
	IntentKind       = "AgentIntent"
	PlanKind         = "AgentCreationPlan"
)

type AgentIntent struct {
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	Kind       string         `json:"kind" yaml:"kind"`
	Metadata   IntentMetadata `json:"metadata" yaml:"metadata"`
	Spec       IntentSpec     `json:"spec" yaml:"spec"`
}

type IntentMetadata struct {
	Name string `json:"name" yaml:"name"`
}

type IntentSpec struct {
	Description   string           `json:"description,omitempty" yaml:"description,omitempty"`
	Instructions  string           `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Runtime       string           `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	KagentRuntime string           `json:"kagentRuntime,omitempty" yaml:"kagentRuntime,omitempty"`
	Namespace     string           `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Provider      ProviderIntent   `json:"provider" yaml:"provider"`
	Tools         []string         `json:"tools,omitempty" yaml:"tools,omitempty"`
	Skills        []string         `json:"skills,omitempty" yaml:"skills,omitempty"`
	Task          string           `json:"task,omitempty" yaml:"task,omitempty"`
	Execution     ExecutionIntent  `json:"execution,omitempty" yaml:"execution,omitempty"`
	Deployment    DeploymentIntent `json:"deployment,omitempty" yaml:"deployment,omitempty"`
}

type ProviderIntent struct {
	Type      string          `json:"type,omitempty" yaml:"type,omitempty"`
	Model     string          `json:"model,omitempty" yaml:"model,omitempty"`
	BaseURL   string          `json:"baseURL,omitempty" yaml:"baseURL,omitempty"`
	SecretRef SecretRefIntent `json:"secretRef" yaml:"secretRef"`
}

type SecretRefIntent struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	Key  string `json:"key,omitempty" yaml:"key,omitempty"`
}

type ExecutionIntent struct {
	Sandbox        string `json:"sandbox,omitempty" yaml:"sandbox,omitempty"`
	Language       string `json:"language,omitempty" yaml:"language,omitempty"`
	Shell          bool   `json:"shell,omitempty" yaml:"shell,omitempty"`
	NativePackages bool   `json:"nativePackages,omitempty" yaml:"nativePackages,omitempty"`
	ContainerImage bool   `json:"containerImage,omitempty" yaml:"containerImage,omitempty"`
	DeviceAccess   bool   `json:"deviceAccess,omitempty" yaml:"deviceAccess,omitempty"`
}

type DeploymentIntent struct {
	Mode       string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Output     string `json:"output,omitempty" yaml:"output,omitempty"`
	BundlePath string `json:"bundlePath,omitempty" yaml:"bundlePath,omitempty"`
}

type Question struct {
	ID       string   `json:"id"`
	Prompt   string   `json:"prompt"`
	Choices  []string `json:"choices,omitempty"`
	Required bool     `json:"required"`
}

type Mutation struct {
	Action    string `json:"action"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type AgentCreationPlan struct {
	APIVersion string                    `json:"apiVersion"`
	Kind       string                    `json:"kind"`
	Intent     AgentIntent               `json:"intent"`
	Runtime    string                    `json:"runtime"`
	Sandbox    *agentruntime.SandboxSpec `json:"sandbox,omitempty"`
	Mutations  []Mutation                `json:"mutations"`
	Digest     string                    `json:"digest"`
}

type PlanResult struct {
	Status    string             `json:"status"`
	Questions []Question         `json:"questions,omitempty"`
	Plan      *AgentCreationPlan `json:"plan,omitempty"`
}

type ApplyRequest struct {
	Plan           AgentCreationPlan `json:"plan"`
	ApprovedDigest string            `json:"approvedDigest"`
}

type ApplyResult struct {
	Status     string `json:"status"`
	PlanDigest string `json:"planDigest"`
	Mode       string `json:"mode"`
	Output     string `json:"output,omitempty"`
	BundlePath string `json:"bundlePath,omitempty"`
}

func IntentJSONSchema() map[string]any {
	stringProperty := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	booleanProperty := func(description string) map[string]any {
		return map[string]any{"type": "boolean", "description": description}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"apiVersion", "kind", "metadata", "spec"},
		"properties": map[string]any{
			"apiVersion": map[string]any{"const": IntentAPIVersion},
			"kind":       map[string]any{"const": IntentKind},
			"metadata": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"name"},
				"properties": map[string]any{"name": stringProperty("Kubernetes-compatible agent name")},
			},
			"spec": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"description":   stringProperty("One-line agent description"),
					"instructions":  stringProperty("System instructions"),
					"runtime":       map[string]any{"type": "string", "enum": []string{"orka", "kagent"}},
					"kagentRuntime": map[string]any{"type": "string", "enum": []string{"go", "python"}},
					"namespace":     stringProperty("Explicit target namespace"),
					"provider": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"type":    map[string]any{"type": "string", "enum": []string{"openai", "anthropic", "azure-openai"}},
							"model":   stringProperty("Provider model identifier"),
							"baseURL": stringProperty("Optional provider endpoint"),
							"secretRef": map[string]any{
								"type": "object", "additionalProperties": false,
								"properties": map[string]any{
									"name": stringProperty("Existing Secret name; never a credential value"),
									"key":  stringProperty("Key within the existing Secret"),
								},
							},
						},
					},
					"tools":  map[string]any{"type": "array", "items": stringProperty("Explicit tool name")},
					"skills": map[string]any{"type": "array", "items": stringProperty("Explicit skill name")},
					"task":   stringProperty("Optional first prompt"),
					"execution": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"sandbox":        map[string]any{"type": "string", "enum": []string{"auto", "hyperlight-js", "unikraft", "pod"}},
							"language":       stringProperty("Language used by generated code or tools"),
							"shell":          booleanProperty("Requires a Linux shell"),
							"nativePackages": booleanProperty("Requires native or dynamically installed packages"),
							"containerImage": booleanProperty("Requires an OCI image"),
							"deviceAccess":   booleanProperty("Requires a device such as a GPU"),
						},
					},
					"deployment": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"mode":       map[string]any{"type": "string", "enum": []string{"offline", "cluster"}},
							"output":     stringProperty("Review artifact path"),
							"bundlePath": stringProperty("Portable bundle directory"),
						},
					},
				},
			},
		},
	}
}
