package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentcreator"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentdriver"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
)

func newAgentCreatorCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{
		Use:   "creator",
		Short: "Turn a natural-language request into a reviewed, digest-gated agent creation plan",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	group.AddCommand(
		newAgentCreatorDraftCommand(state),
		newAgentCreatorPlanCommand(state),
		newAgentCreatorApplyCommand(state),
		newAgentCreatorMCPCommand(state),
	)
	return group
}

func newAgentCreatorDraftCommand(state *commandState) *cobra.Command {
	var driver, description, descriptionFile, out, copilotCLI, claudeCLI string
	cmd := &cobra.Command{
		Use:   "draft",
		Short: "Ask Copilot or Claude to draft a schema-bound AgentIntent",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&driver, "driver", "", "coding agent driver: copilot or claude (required)")
	cmd.Flags().StringVar(&description, "description", "", "natural-language agent request")
	cmd.Flags().StringVar(&descriptionFile, "description-file", "", "file containing the natural-language request ('-' for stdin)")
	cmd.Flags().StringVarP(&out, "out", "o", "-", "AgentIntent JSON output path ('-' for stdout)")
	cmd.Flags().StringVar(&copilotCLI, "copilot-cli", "copilot", "Copilot CLI executable")
	cmd.Flags().StringVar(&claudeCLI, "claude-cli", "claude", "Claude Code executable")
	cmd.MarkFlagsOneRequired("description", "description-file")
	cmd.MarkFlagsMutuallyExclusive("description", "description-file")
	_ = cmd.RegisterFlagCompletionFunc("driver", staticCompletion([]string{"copilot", "claude"}))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if driver != "copilot" && driver != "claude" {
			return fmt.Errorf("--driver must be copilot or claude")
		}
		text := description
		if descriptionFile != "" {
			raw, err := readAgentCreatorInput(state, descriptionFile)
			if err != nil {
				return err
			}
			text = string(raw)
		}
		intent, err := agentdriver.Draft(cmd.Context(), agentdriver.Options{
			Driver: driver, Description: text,
			CopilotExecutable: copilotCLI, ClaudeExecutable: claudeCLI,
		})
		if err != nil {
			return err
		}
		return writeAgentCreatorJSON(state.deps.stdout, out, intent)
	}
	return cmd
}

func newAgentCreatorPlanCommand(state *commandState) *cobra.Command {
	var intentPath, out string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Validate an AgentIntent and produce an immutable review plan",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&intentPath, "intent", "-", "AgentIntent JSON or YAML path ('-' for stdin)")
	cmd.Flags().StringVarP(&out, "out", "o", "-", "plan JSON output path ('-' for stdout)")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		raw, err := readAgentCreatorInput(state, intentPath)
		if err != nil {
			return err
		}
		var intent agentcreator.AgentIntent
		if err := decodeAgentCreatorDocument(raw, &intent); err != nil {
			return fmt.Errorf("decode AgentIntent: %w", err)
		}
		result, err := agentcreator.BuildPlan(intent)
		if err != nil {
			return err
		}
		return writeAgentCreatorJSON(state.deps.stdout, out, result)
	}
	return cmd
}

func newAgentCreatorApplyCommand(state *commandState) *cobra.Command {
	var planPath, approvedDigest string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply an immutable plan only when its reviewed digest is supplied",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&planPath, "plan", "", "AgentCreationPlan JSON path (required)")
	cmd.Flags().StringVar(&approvedDigest, "approve", "", "exact digest copied from the reviewed plan (required)")
	_ = cmd.MarkFlagRequired("plan")
	_ = cmd.MarkFlagRequired("approve")
	cmd.RunE = appRun(state, func(a *app.App) error {
		raw, err := readAgentCreatorInput(state, planPath)
		if err != nil {
			return err
		}
		plan, err := decodeAgentCreatorPlan(raw)
		if err != nil {
			return fmt.Errorf("decode AgentCreationPlan: %w", err)
		}
		request := agentcreator.ApplyRequest{Plan: plan, ApprovedDigest: approvedDigest}
		if err := agentcreator.VerifyApplyRequest(request); err != nil {
			return err
		}
		result, err := applyAgentCreatorPlan(a, request)
		if err != nil {
			return err
		}
		return writeAgentCreatorJSON(state.deps.stdout, "-", result)
	})
	return cmd
}

func newAgentCreatorMCPCommand(state *commandState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve KMX agent planning and digest-gated apply tools over MCP stdio",
		Args:  cobra.NoArgs,
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return agentcreator.RunMCPServer(cmd.Context(), func(_ context.Context, request agentcreator.ApplyRequest) (agentcreator.ApplyResult, error) {
			if request.Plan.Intent.Spec.Deployment.Output == "-" {
				return agentcreator.ApplyResult{}, fmt.Errorf("MCP apply requires a file output path; stdout is reserved for the protocol")
			}
			a, err := state.application()
			if err != nil {
				return agentcreator.ApplyResult{}, err
			}
			a.Out = io.Discard
			a.Run.Stdout = io.Discard
			return applyAgentCreatorPlan(a, request)
		})
	}
	return cmd
}

func applyAgentCreatorPlan(a *app.App, request agentcreator.ApplyRequest) (agentcreator.ApplyResult, error) {
	intent := request.Plan.Intent
	noApply := intent.Spec.Deployment.Mode == "offline"
	opt := app.CreateOptions{
		Name:                  intent.Metadata.Name,
		Namespace:             intent.Spec.Namespace,
		Description:           intent.Spec.Description,
		ProviderType:          intent.Spec.Provider.Type,
		Model:                 intent.Spec.Provider.Model,
		BaseURL:               intent.Spec.Provider.BaseURL,
		Secret:                intent.Spec.Provider.SecretRef.Name,
		SecretKey:             intent.Spec.Provider.SecretRef.Key,
		InstructionText:       intent.Spec.Instructions,
		Tools:                 strings.Join(intent.Spec.Tools, ","),
		Skills:                strings.Join(intent.Spec.Skills, ","),
		Task:                  intent.Spec.Task,
		Runtime:               intent.Spec.Runtime,
		KagentRuntime:         intent.Spec.KagentRuntime,
		Sandbox:               intent.Spec.Execution.Sandbox,
		SandboxLanguage:       intent.Spec.Execution.Language,
		SandboxShell:          intent.Spec.Execution.Shell,
		SandboxNativePackages: intent.Spec.Execution.NativePackages,
		SandboxContainerImage: intent.Spec.Execution.ContainerImage,
		SandboxDeviceAccess:   intent.Spec.Execution.DeviceAccess,
		Out:                   intent.Spec.Deployment.Output,
		BundlePath:            intent.Spec.Deployment.BundlePath,
		NoApply:               noApply,
	}
	if err := a.CreateAgent(opt); err != nil {
		return agentcreator.ApplyResult{}, err
	}
	status := "applied"
	if noApply {
		status = "written"
	}
	return agentcreator.ApplyResult{
		Status: status, PlanDigest: request.Plan.Digest, Mode: intent.Spec.Deployment.Mode,
		Output: intent.Spec.Deployment.Output, BundlePath: intent.Spec.Deployment.BundlePath,
	}, nil
}

func decodeAgentCreatorPlan(raw []byte) (agentcreator.AgentCreationPlan, error) {
	var result agentcreator.PlanResult
	if err := decodeAgentCreatorDocument(raw, &result); err == nil && result.Plan != nil {
		return *result.Plan, nil
	}
	var plan agentcreator.AgentCreationPlan
	if err := decodeAgentCreatorDocument(raw, &plan); err != nil {
		return plan, err
	}
	return plan, nil
}

func readAgentCreatorInput(state *commandState, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(io.LimitReader(state.deps.stdin, 1<<20))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("%s exceeds 1 MiB", path)
	}
	return data, nil
}

func decodeAgentCreatorDocument(raw []byte, destination any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("document is empty")
	}
	if trimmed[0] == '{' {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(destination); err != nil {
			return err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return fmt.Errorf("expected exactly one JSON document")
		}
		return nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(trimmed))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one YAML document")
	}
	return nil
}

func writeAgentCreatorJSON(stdout io.Writer, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "-" {
		_, err = stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
