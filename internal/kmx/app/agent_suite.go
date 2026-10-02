package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

type PackageAgentSuiteOptions struct {
	Output string
}

type RunAgentSuiteOptions struct {
	AgentSessionsServer  string
	AgentSessionsProject string
	Prompt               string
	PromptFile           string
	Wait                 time.Duration
}

func (a *App) InspectAgentSuite(path string) error {
	suite, err := agentsuite.LoadInspectable(path)
	if err != nil {
		return err
	}
	digest, err := suite.Digest()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "Suite:       %s\n", suite.Name)
	fmt.Fprintf(a.Out, "Digest:      %s\n", digest)
	fmt.Fprintf(a.Out, "Entrypoint:  %s\n", suite.Entrypoint)
	fmt.Fprintf(a.Out, "Runtime:     %s\n", suite.Runtime)
	fmt.Fprintf(a.Out, "Aggregation: %s <- %s\n\n", suite.Aggregation.Member, strings.Join(suite.Aggregation.Inputs, ", "))
	fmt.Fprintln(a.Out, "Members:")
	for _, member := range suite.Members {
		fmt.Fprintf(a.Out, "  %-24s %-11s %s\n", member.Name, member.Role, member.PortableDigest)
	}
	fmt.Fprintln(a.Out, "\nRead-only Substrate lift plan:")
	for _, member := range suite.Members {
		fmt.Fprintf(a.Out, "  verify  %-24s harness=chat actor-template=%s\n", member.Name, member.Name)
	}
	fmt.Fprintln(a.Out, "  configure AgentSessions member metadata")
	fmt.Fprintln(a.Out, "  create Actors lazily when member sessions execute")
	return nil
}

func (a *App) PackageAgentSuite(path string, opt PackageAgentSuiteOptions) error {
	suite, err := agentsuite.Load(path)
	if err != nil {
		return err
	}
	result, err := agentsuite.ExportOCI(suite, opt.Output)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "Suite:           %s\n", suite.Name)
	fmt.Fprintf(a.Out, "Members:         %d\n", len(suite.Members))
	fmt.Fprintf(a.Out, "OCI layout:      %s\n", result.Layout)
	fmt.Fprintf(a.Out, "Suite digest:    %s\n", result.SuiteDigest)
	fmt.Fprintf(a.Out, "Manifest digest: %s\n", result.ManifestDigest)
	return nil
}

func (a *App) RunAgentSuite(path string, opt RunAgentSuiteOptions) error {
	if a.Run == nil || a.Out == nil || a.Err == nil {
		return fmt.Errorf("Agent Suite run requires configured process runner and streams")
	}
	if strings.TrimSpace(opt.AgentSessionsServer) == "" {
		return fmt.Errorf("Agent Suite run requires --server")
	}
	if err := scaffold.RefuseKeyShapes(opt.AgentSessionsServer); err != nil {
		return fmt.Errorf("refusing credential-shaped AgentSessions server address")
	}
	if opt.AgentSessionsProject == "" {
		opt.AgentSessionsProject = "default"
	}
	if err := scaffold.RefuseKeyShapes(opt.AgentSessionsProject); err != nil {
		return fmt.Errorf("refusing credential-shaped AgentSessions project")
	}
	if (opt.Prompt == "") == (opt.PromptFile == "") {
		return fmt.Errorf("supply exactly one of --prompt or --prompt-file")
	}
	prompt, err := resolveRunPrompt(a, RunAgentOptions{Prompt: opt.Prompt, PromptFile: opt.PromptFile})
	if err != nil {
		return err
	}
	timeout, err := orkaResultDeadline(opt.Wait)
	if err != nil {
		return err
	}
	suite, err := agentsuite.Load(path)
	if err != nil {
		return err
	}
	suiteDigest, err := suite.Digest()
	if err != nil {
		return err
	}
	runUID, err := newSuiteRunUID()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), timeout)
	defer cancel()

	results := make(map[string]string, len(suite.Aggregation.Inputs))
	for _, name := range suite.Aggregation.Inputs {
		member, _ := suite.Member(name)
		memberPrompt := fmt.Sprintf(
			"Suite %s delegated this request from coordinator %s.\n\nRequest:\n%s\n\nReturn your specialist evidence for the coordinator to aggregate.",
			suite.Name, suite.Entrypoint, prompt,
		)
		answer, sessionUID, err := a.runSuiteMember(ctx, suite, suiteDigest, runUID, member, memberPrompt, map[string]string{
			"kmx.kaimahi.dev/handoff-from": suite.Entrypoint,
			"kmx.kaimahi.dev/run-stage":    "specialist",
		}, opt)
		if err != nil {
			return fmt.Errorf("run suite member %s: %w", name, err)
		}
		results[name] = answer
		if err := a.suspendSuiteMember(ctx, sessionUID, opt); err != nil {
			return fmt.Errorf("suspend suite member %s: %w", name, err)
		}
	}

	coordinator, _ := suite.Member(suite.Aggregation.Member)
	var aggregate strings.Builder
	fmt.Fprintf(&aggregate, "Original request:\n%s\n\nDurable specialist results:\n", prompt)
	for _, name := range suite.Aggregation.Inputs {
		fmt.Fprintf(&aggregate, "\n[%s]\n%s\n", name, results[name])
	}
	fmt.Fprintf(&aggregate, "\nSynthesize one final answer. Preserve important disagreements and identify which specialist supplied each material fact.")
	answer, _, err := a.runSuiteMember(ctx, suite, suiteDigest, runUID, coordinator, aggregate.String(), map[string]string{
		"kmx.kaimahi.dev/aggregation-strategy": suite.Aggregation.Strategy,
		"kmx.kaimahi.dev/run-stage":            "aggregate",
	}, opt)
	if err != nil {
		return fmt.Errorf("aggregate suite results: %w", err)
	}
	_, err = io.WriteString(a.Out, answer)
	return err
}

func (a *App) runSuiteMember(
	ctx context.Context,
	suite *agentsuite.Resolved,
	suiteDigest, runUID string,
	member agentsuite.ResolvedMember,
	prompt string,
	extra map[string]string,
	opt RunAgentSuiteOptions,
) (string, string, error) {
	annotations := map[string]string{
		"kmx.kaimahi.dev/suite-digest":    suiteDigest,
		"kmx.kaimahi.dev/agent-digest":    member.PortableDigest,
		"kmx.kaimahi.dev/suite-member":    member.Name,
		"kmx.kaimahi.dev/run-uid":         runUID,
		"kmx.kaimahi.dev/runtime":         "agentsessions",
		"kmx.kaimahi.dev/compute-runtime": suite.Runtime,
	}
	for key, value := range extra {
		annotations[key] = value
	}
	turn := agentSessionsTurn{
		Name:        member.Name,
		Model:       member.Model,
		Annotations: annotations,
		Messages: []agentSessionsTurnMessage{
			{Role: "system", Text: member.Agent().Spec.Instructions},
			{Role: "user", Text: prompt},
		},
	}
	payload, err := json.Marshal(turn)
	if err != nil {
		return "", "", fmt.Errorf("encode member turn: %w", err)
	}
	runner := *a.Run
	runner.Context = ctx
	runner.Stdout = nil
	var diagnostics bytes.Buffer
	runner.Stderr = io.MultiWriter(a.Err, &diagnostics)
	var answer bytes.Buffer
	err = runner.Pipe(bytes.NewReader(payload), &answer, "agentctl", "exec",
		"--server", opt.AgentSessionsServer,
		"--project", opt.AgentSessionsProject,
		"--harness", "chat",
		"--turn-file", "-",
		"--output-only",
	)
	if err != nil {
		return "", "", err
	}
	if answer.Len() == 0 {
		return "", "", fmt.Errorf("AgentSessions returned no answer")
	}
	sessionUID := suiteSessionUID(diagnostics.String())
	if sessionUID == "" {
		return "", "", fmt.Errorf("AgentSessions returned no session identity")
	}
	return answer.String(), sessionUID, nil
}

func (a *App) suspendSuiteMember(ctx context.Context, sessionUID string, opt RunAgentSuiteOptions) error {
	runner := *a.Run
	runner.Context = ctx
	runner.Stdout = nil
	runner.Stderr = a.Err
	return runner.Pipe(nil, io.Discard, "agentctl", "suspend",
		"--server", opt.AgentSessionsServer,
		"--project", opt.AgentSessionsProject,
		"--session", sessionUID,
	)
}

func suiteSessionUID(diagnostics string) string {
	for _, line := range strings.Split(diagnostics, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "session "); ok {
			if uid, _, _ := strings.Cut(value, " "); uid != "" {
				return uid
			}
		}
	}
	return ""
}

func newSuiteRunUID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create suite run identity: %w", err)
	}
	return "run-" + hex.EncodeToString(value[:]), nil
}
