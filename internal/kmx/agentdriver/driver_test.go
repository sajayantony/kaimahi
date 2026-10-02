package agentdriver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const intentJSON = `{"apiVersion":"kmx.kaimahi.dev/v1alpha1","kind":"AgentIntent","metadata":{"name":"reviewer"},"spec":{"namespace":"agents","provider":{"type":"openai","model":"gpt-5","secretRef":{"name":"model-key"}},"execution":{"sandbox":"auto","language":"javascript"},"deployment":{"mode":"offline"}}}`

func TestDraftWithCopilotAndClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args")
	copilot := filepath.Join(dir, "copilot")
	claude := filepath.Join(dir, "claude")
	encodedIntent, err := json.Marshal(intentJSON)
	if err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, copilot, `printf '%s\n' "$@" > "$ARGS_LOG"
printf '%s\n' `+shellSingleQuote(fmt.Sprintf(`{"type":"assistant.message","data":{"content":%s}}`, encodedIntent))+` '{"type":"result","exitCode":0}'`)
	writeExecutable(t, claude, `printf '%s\n' "$@" > "$ARGS_LOG"
printf '%s\n' '{"structured_output":`+intentJSON+`}'`)
	t.Setenv("ARGS_LOG", argsLog)

	for _, tc := range []struct {
		name   string
		driver string
		path   string
	}{
		{"copilot", "copilot", copilot},
		{"claude", "claude", claude},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := Options{Driver: tc.driver, Description: "Create a reviewer"}
			if tc.driver == "copilot" {
				options.CopilotExecutable = tc.path
			} else {
				options.ClaudeExecutable = tc.path
			}
			intent, err := Draft(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			if intent.Metadata.Name != "reviewer" {
				t.Fatal(intent)
			}
			args, err := os.ReadFile(argsLog)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(args), "--output-format") {
				t.Fatal(string(args))
			}
		})
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
