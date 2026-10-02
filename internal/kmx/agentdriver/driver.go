package agentdriver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentcreator"
)

type Options struct {
	Driver            string
	Description       string
	CopilotExecutable string
	ClaudeExecutable  string
}

func Draft(ctx context.Context, options Options) (agentcreator.AgentIntent, error) {
	if strings.TrimSpace(options.Description) == "" {
		return agentcreator.AgentIntent{}, fmt.Errorf("agent description is required")
	}
	schema, err := json.Marshal(agentcreator.IntentJSONSchema())
	if err != nil {
		return agentcreator.AgentIntent{}, err
	}
	prompt := "Turn the user request into one KMX AgentIntent JSON object matching this JSON Schema. " +
		"Use Secret references only; never invent or request credential values. " +
		"Do not choose a sandbox for preference: state workload requirements and use execution.sandbox=auto. " +
		"Use deployment.mode=offline unless the user explicitly requested cluster deployment. " +
		"Return only the JSON object.\n\nJSON Schema:\n" + string(schema) + "\n\nUser request:\n" + options.Description

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	switch options.Driver {
	case "copilot":
		executable := options.CopilotExecutable
		if executable == "" {
			executable = "copilot"
		}
		raw, err := runBounded(ctx, executable,
			"-p", prompt, "--model", "auto", "--silent", "--stream", "off",
			"--output-format", "json", "--available-tools=kmx_tool_adapter_only",
			"--disable-builtin-mcps", "--no-custom-instructions", "--no-ask-user", "--no-auto-update")
		if err != nil {
			return agentcreator.AgentIntent{}, fmt.Errorf("Copilot agent creator failed: %w", err)
		}
		return decodeCopilotIntent(raw)
	case "claude":
		executable := options.ClaudeExecutable
		if executable == "" {
			executable = "claude"
		}
		raw, err := runBounded(ctx, executable,
			"-p", prompt, "--output-format", "json", "--json-schema", string(schema),
			"--permission-mode", "dontAsk", "--allowedTools", "", "--no-session-persistence")
		if err != nil {
			return agentcreator.AgentIntent{}, fmt.Errorf("Claude agent creator failed: %w", err)
		}
		return decodeClaudeIntent(raw)
	default:
		return agentcreator.AgentIntent{}, fmt.Errorf("driver must be copilot or claude")
	}
}

func runBounded(ctx context.Context, executable string, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "kmx-agent-creator-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	stdout := &limitedBuffer{remaining: 1 << 20}
	stderr := &limitedBuffer{remaining: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	if stdout.exceeded {
		return nil, fmt.Errorf("driver output exceeds 1 MiB")
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	exceeded  bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if len(p) > b.remaining {
		p = p[:max(b.remaining, 0)]
		b.exceeded = true
	}
	b.remaining -= len(p)
	_, _ = b.buffer.Write(p)
	return original, nil
}

func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }

func decodeCopilotIntent(raw []byte) (agentcreator.AgentIntent, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	answer := ""
	for {
		var event struct {
			Type string `json:"type"`
			Data struct {
				Content      string            `json:"content"`
				ToolRequests []json.RawMessage `json:"toolRequests"`
			} `json:"data"`
			ExitCode int `json:"exitCode"`
		}
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return agentcreator.AgentIntent{}, fmt.Errorf("invalid Copilot JSON output")
		}
		if event.Type == "assistant.message" {
			if len(event.Data.ToolRequests) > 0 {
				return agentcreator.AgentIntent{}, fmt.Errorf("Copilot attempted a tool while drafting an AgentIntent")
			}
			answer = event.Data.Content
		}
		if event.Type == "result" && event.ExitCode != 0 {
			return agentcreator.AgentIntent{}, fmt.Errorf("Copilot reported a failed turn")
		}
	}
	return decodeIntentJSON([]byte(answer))
}

func decodeClaudeIntent(raw []byte) (agentcreator.AgentIntent, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return agentcreator.AgentIntent{}, fmt.Errorf("invalid Claude JSON output")
	}
	if structured := envelope["structured_output"]; len(structured) > 0 && string(structured) != "null" {
		return decodeIntentJSON(structured)
	}
	if result := envelope["result"]; len(result) > 0 {
		var text string
		if json.Unmarshal(result, &text) == nil {
			return decodeIntentJSON([]byte(text))
		}
		return decodeIntentJSON(result)
	}
	return decodeIntentJSON(raw)
}

func decodeIntentJSON(raw []byte) (agentcreator.AgentIntent, error) {
	text := strings.TrimSpace(string(raw))
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	if strings.IndexFunc(text, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	}) >= 0 {
		return agentcreator.AgentIntent{}, fmt.Errorf("driver returned control characters")
	}
	var intent agentcreator.AgentIntent
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(text)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return intent, fmt.Errorf("driver did not return a valid AgentIntent: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return intent, fmt.Errorf("driver returned trailing JSON data")
	}
	if _, err := agentcreator.BuildPlan(intent); err != nil {
		return intent, fmt.Errorf("driver returned an invalid AgentIntent: %w", err)
	}
	return intent, nil
}
