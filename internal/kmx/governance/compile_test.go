package governance

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
)

func TestPolicyCompilerFailsClosedAndBindsArtifacts(t *testing.T) {
	raw, err := os.ReadFile("../../../agentsuite/policy/testdata/deny-all.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := agentsuite.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Compile(document, Backend)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Compile(document, Backend)
	if err != nil || again.PolicyDigest != plan.PolicyDigest || again.ProfileDigest != plan.ProfileDigest {
		t.Fatal("unstable policy identity")
	}
	if plan.Gateway.Routes[0].Policies.Authorization.Rules[0].Require != "false" {
		t.Fatal("deny-all gateway must require false, not rely on empty allow rules")
	}
	if !strings.Contains(plan.Launcher, "abi < 3") || !strings.Contains(plan.Launcher, "os.execvp") {
		t.Fatal("missing fail-closed filesystem launcher")
	}
	sendto := plan.Seccomp.Syscalls[len(plan.Seccomp.Syscalls)-1]
	if len(sendto.Names) != 1 || sendto.Names[0] != "sendto" || len(sendto.Args) != 1 ||
		sendto.Args[0].Index != 4 || sendto.Args[0].Value != 0 || sendto.Args[0].Op != "SCMP_CMP_NE" {
		t.Fatal("explicit datagram destinations must be denied while accepted-socket replies remain usable")
	}
	encoded, _ := json.Marshal(plan.Seccomp)
	for _, call := range []string{`"connect"`, `"sendto"`, `"sendmsg"`, `"openat2"`, `"io_uring_setup"`, `"renameat2"`, `"socketcall"`} {
		if !strings.Contains(string(encoded), call) {
			t.Fatalf("missing denied syscall %s", call)
		}
	}
	if _, err := Compile(document, "observation-only"); err == nil {
		t.Fatal("accepted unsupported enforcement")
	}
	document.Network.Allow = []policy.Destination{{Scheme: "https", Host: "example.com", Port: 443}}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	if plan, err := Compile(document, Backend); err == nil || plan.PolicyDigest != "" {
		t.Fatal("allowlist must fail without a success-shaped partial plan")
	}
	document.Network.Allow = []policy.Destination{}
	document.Invocations.Allow = []policy.Invocation{{Agent: "peer", Skills: []string{"summarize"}}}
	if _, err := Compile(document, Backend); err == nil {
		t.Fatal("accepted unenforced invocation grant")
	}
}
