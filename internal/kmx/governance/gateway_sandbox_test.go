package governance

import (
	"slices"
	"testing"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
)

func TestGatewaySandboxProjectionIsNotFullCompilation(t *testing.T) {
	document := policy.Document{
		APIVersion: policy.Version, Agent: "reader",
		Capabilities: policy.Capabilities{Protocol: "a2a", Version: "1.0.0", Skills: []policy.Skill{}},
		Filesystem:   policy.Filesystem{Write: "deny"},
		Network:      policy.Network{Default: "deny", Allow: []policy.Destination{{Scheme: "https", Host: "mcr.microsoft.com", Port: 443}}},
		Invocations:  policy.Invocations{Default: "deny", Allow: []policy.Invocation{}},
	}
	projection, err := ProjectGatewaySandbox(document)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Scope != "filesystem-and-local-socket-controls-only" || len(projection.Required) != 4 {
		t.Fatal("partial projection must report required external controls")
	}
	if slices.Contains(projection.Seccomp.Syscalls[0].Names, "connect") ||
		!slices.Contains(projection.Seccomp.Syscalls[0].Names, "chmod") ||
		projection.Seccomp.Syscalls[2].Args[0].Value != 1 {
		t.Fatal("projection must permit TCP but retain mutation and Unix-socket denial")
	}
	if _, err := Compile(document, Backend); err == nil {
		t.Fatal("whole-policy backend must continue rejecting allowlists")
	}
	document.Filesystem.Write = "allow"
	if _, err := ProjectGatewaySandbox(document); err == nil {
		t.Fatal("invalid policy must not produce artifacts")
	}
}
