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

func TestRunAgentSuiteAggregatesDurableMemberResults(t *testing.T) {
	temp := t.TempDir()
	bin := filepath.Join(temp, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
	if [ "$1" = "suspend" ]; then
	  exit 0
	fi
	n=0
	if [ -f "$COUNT" ]; then n=$(cat "$COUNT"); fi
	n=$((n + 1))
	printf '%s' "$n" > "$COUNT"
	cat > "$CAPTURE/turn-$n.json"
	printf 'session session-%s\n' "$n" >&2
	case "$n" in
  1) printf 'purchasing evidence\n' ;;
  2) printf 'receiving evidence\n' ;;
  3) printf 'aggregated recommendation\n' ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "agentctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	a := &App{
		Run: &run.Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Echo:   true,
			Env: []string{
				"COUNT=" + filepath.Join(temp, "count"),
				"CAPTURE=" + temp,
			},
		},
		Out:   &stdout,
		Err:   &stderr,
		Stdin: os.Stdin,
	}
	err := a.RunAgentSuite(sampleAgentSuitePath(t), RunAgentSuiteOptions{
		AgentSessionsServer:  "127.0.0.1:18080",
		AgentSessionsProject: "suite-poc",
		Prompt:               "Should invoice 1042 be approved?",
		Wait:                 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "aggregated recommendation\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}

	var turns []agentSessionsTurn
	for i := 1; i <= 3; i++ {
		body, err := os.ReadFile(filepath.Join(temp, "turn-"+string(rune('0'+i))+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var turn agentSessionsTurn
		if err := json.Unmarshal(body, &turn); err != nil {
			t.Fatal(err)
		}
		turns = append(turns, turn)
	}
	if turns[0].Name != "purchasing-agent" || turns[1].Name != "receiving-agent" || turns[2].Name != "invoice-coordinator" {
		t.Fatalf("member order = %q, %q, %q", turns[0].Name, turns[1].Name, turns[2].Name)
	}
	runUID := turns[0].Annotations["kmx.kaimahi.dev/run-uid"]
	for _, turn := range turns {
		if turn.Annotations["kmx.kaimahi.dev/suite-digest"] == "" ||
			turn.Annotations["kmx.kaimahi.dev/agent-digest"] == "" ||
			turn.Annotations["kmx.kaimahi.dev/run-uid"] != runUID {
			t.Fatalf("annotations = %+v", turn.Annotations)
		}
	}
	aggregate := turns[2].Messages[1].Text
	if !strings.Contains(aggregate, "[purchasing-agent]\npurchasing evidence") ||
		!strings.Contains(aggregate, "[receiving-agent]\nreceiving evidence") {
		t.Fatalf("aggregation prompt = %q", aggregate)
	}
}

func sampleAgentSuitePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "agent-suite", "invoice-review"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}
