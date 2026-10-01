package app

import (
	"fmt"
	"strings"

	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

// CreateOptions accepts references, never credential values. Raw numeric flags
// preserve the distinction between an omitted limit and an explicit zero.
type CreateOptions struct {
	Name, Namespace, Description, ProviderType, Model, BaseURL, AzureDeployment, AzureAPIVersion, Secret, SecretKey string
	Instructions, InstructionText, Tools, Skills, Task                                                              string
	AllowedAgents                                                                                                   []string
	AgentRequestsPerMinute, AgentTokensPerMinute                                                                    string
	ProviderRequestsPerMinute, ProviderTokensPerMinute                                                              string
	ResultServiceAccount, OrkaAPIService, ResultPort, SchemaTarget                                                  string
	Out, BundlePath, Runtime, KagentRuntime                                                                         string
	Sandbox, SandboxLanguage                                                                                        string
	SandboxShell, SandboxNativePackages, SandboxContainerImage, SandboxDeviceAccess                                 bool
	NoApply, DryRun, Coordination                                                                                   bool
	sandboxPlan                                                                                                     *agentruntime.SandboxPlan
	// Resolved before entering raw terminal mode. Keep the original flags and
	// distinguish an empty file from an instruction source not yet read.
	instructionFileText *string
	descriptionDefault  string
}

// CreateAgent selects an explicit creation runtime. Empty remains Orka so
// callers that construct a zero-value CreateOptions retain the original path.
func (a *App) CreateAgent(opt CreateOptions) error {
	plan, err := agentruntime.SelectSandbox(opt.Sandbox, agentruntime.SandboxRequirements{
		Language:       opt.SandboxLanguage,
		Shell:          opt.SandboxShell,
		NativePackages: opt.SandboxNativePackages,
		ContainerImage: opt.SandboxContainerImage,
		DeviceAccess:   opt.SandboxDeviceAccess,
	})
	if err != nil {
		return err
	}
	opt.sandboxPlan = plan
	if plan != nil {
		a.notef("Sandbox plan: %s — %s.", plan.Spec.Backend, plan.Reason)
	}
	switch strings.TrimSpace(opt.Runtime) {
	case "", "orka":
		return a.createOrkaAgent(opt)
	case "kagent":
		return a.createKagentAgent(opt)
	default:
		return fmt.Errorf("unsupported agent runtime; use orka or kagent")
	}
}

// serverCondition is the shared subset of Kubernetes status conditions used
// by the Orka and Kagent create readers and by agent show.
type serverCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	// LastTransitionTime distinguishes a live verdict from one about a
	// credential that has since been replaced.
	LastTransitionTime string `json:"lastTransitionTime"`
	ObservedGeneration int64  `json:"observedGeneration"`
}
