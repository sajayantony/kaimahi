package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

func TestRunAgentSessionsSendsPortableIdentityAndInstructions(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	if err := os.Mkdir(bundle, 0o700); err != nil {
		t.Fatal(err)
	}
	source := []byte(`apiVersion: kmx.kaimahi.dev/v1alpha1
kind: PortableAgent
metadata:
  name: local-reviewer
spec:
  instructions: Review the request carefully.
  model:
    name: qwen3:8b
extensions: {}
`)
	if err := os.WriteFile(filepath.Join(bundle, "agent.yaml"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "turn.json")
	script := "#!/bin/sh\ncat > \"$CAPTURE\"\necho 'session session-1' >&2\necho 'portable answer'\n"
	if err := os.WriteFile(filepath.Join(bin, "agentctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	a := &App{
		Run:   &run.Runner{Stdout: &stdout, Stderr: &stderr, Env: []string{"CAPTURE=" + capture}, Echo: true},
		Out:   &stdout,
		Err:   &stderr,
		Stdin: os.Stdin,
	}
	err := a.RunAgent(RunAgentOptions{
		BundleDir:            bundle,
		Runtime:              "agentsessions",
		AgentSessionsServer:  "127.0.0.1:8080",
		AgentSessionsProject: "poc",
		Prompt:               "Find the bug.",
		Wait:                 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "portable answer\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	body, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var turn agentSessionsTurn
	if err := json.Unmarshal(body, &turn); err != nil {
		t.Fatal(err)
	}
	if turn.Name != "local-reviewer" || turn.Model != "qwen3:8b" {
		t.Fatalf("identity = %+v", turn)
	}
	if turn.Annotations["kmx.kaimahi.dev/portable-digest"] == "" ||
		turn.Annotations["kmx.kaimahi.dev/runtime"] != "agentsessions" {
		t.Fatalf("annotations = %+v", turn.Annotations)
	}
	if len(turn.Messages) != 2 ||
		turn.Messages[0].Role != "system" ||
		turn.Messages[0].Text != "Review the request carefully." ||
		turn.Messages[1].Text != "Find the bug." {
		t.Fatalf("messages = %+v", turn.Messages)
	}
	if !strings.Contains(stderr.String(), "--turn-file -") {
		t.Fatalf("stderr did not identify the safe invocation: %q", stderr.String())
	}
}

func TestAgentSessionsRejectsOrkaOnlyBehavior(t *testing.T) {
	agent := `apiVersion: kmx.kaimahi.dev/v1alpha1
kind: PortableAgent
metadata:
  name: local-reviewer
spec:
  instructions: Review.
  model:
    name: qwen3
extensions:
  orka:
    apiVersion: core.orka.ai/v1alpha1
    agent:
      tools:
        - name: private-tool
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(agent), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{Run: &run.Runner{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	err := a.RunAgent(RunAgentOptions{
		BundleDir: dir, Runtime: "agentsessions", AgentSessionsServer: "127.0.0.1:8080",
		Prompt: "hello", Wait: 10 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "Orka-only agent behavior") {
		t.Fatalf("error = %v", err)
	}
}
