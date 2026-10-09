package imagelift

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
)

func TestPolicySpikeRemovesWritableTmpAndBindsSubject(t *testing.T) {
	env, record, base := example(t)
	raw, err := os.ReadFile("../../../agentsuite/policy/testdata/deny-all.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := agentsuite.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	document.Agent = record.Agent
	command := []string{"python", "-B", "-c", "print('probe')"}
	plan, enforcement, err := RenderPolicySpike(base.Image, record, env, document, command)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(plan)
	for _, want := range []string{`"readOnlyRootFilesystem":true`, `"runAsUser":65532`, `"localhostProfile"`, enforcement.PolicyDigest, enforcement.NodeLabel} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(string(raw), `"emptyDir"`) || strings.Contains(string(raw), `"mountPath":"/tmp"`) || base.Digest == plan.Digest {
		t.Fatal("policy failed to remove writable tmp or change plan identity")
	}
	again, _, err := RenderPolicySpike(base.Image, record, env, document, command)
	if err != nil || again.Digest != plan.Digest {
		t.Fatal("unstable policy lift plan")
	}
	document.Agent = "another-agent"
	if _, _, err := RenderPolicySpike(base.Image, record, env, document, command); err == nil {
		t.Fatal("accepted policy for another subject")
	}
	unchanged, err := Render(base.Image, record, env)
	if err != nil || unchanged.Digest != base.Digest {
		t.Fatal("experimental policy changed ordinary lift")
	}
}
