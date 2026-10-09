package governance

import "github.com/kaimahi-agents/kaimahi/agentsuite/policy"

// GatewaySandboxProjection supplies only process filesystem controls. Unlike
// Compile, it requires independently installed gateway and network enforcement.
type GatewaySandboxProjection struct {
	Scope         string   `json:"scope"`
	PolicyDigest  string   `json:"policyDigest"`
	ProfileDigest string   `json:"profileDigest"`
	ProfilePath   string   `json:"profilePath"`
	Seccomp       Seccomp  `json:"seccomp"`
	Launcher      string   `json:"launcher"`
	Required      []string `json:"required"`
}

func ProjectGatewaySandbox(document policy.Document) (GatewaySandboxProjection, error) {
	if err := document.Validate(); err != nil {
		return GatewaySandboxProjection{}, err
	}
	policyDigest, err := Digest(document)
	if err != nil {
		return GatewaySandboxProjection{}, err
	}
	return projectFilesystem(policyDigest)
}

func projectFilesystem(policyDigest string) (GatewaySandboxProjection, error) {
	profile := denyProfile()
	names := make([]string, 0, len(profile.Syscalls[0].Names))
	for _, name := range profile.Syscalls[0].Names {
		if name != "connect" {
			names = append(names, name)
		}
	}
	profile.Syscalls[0].Names = names
	profile.Syscalls = append(profile.Syscalls, SyscallRule{
		Names: []string{"socket"}, Action: "SCMP_ACT_ERRNO", ErrnoRet: 1,
		Args: []Argument{{Index: 0, Value: 1, Op: "SCMP_CMP_EQ"}},
	})
	profileDigest, err := Digest(profile)
	if err != nil {
		return GatewaySandboxProjection{}, err
	}
	return GatewaySandboxProjection{
		Scope: "filesystem-and-local-socket-controls-only", PolicyDigest: policyDigest,
		ProfileDigest: profileDigest, ProfilePath: "agentsuite/" + profileDigest[7:] + ".json",
		Seccomp: profile, Launcher: launcher,
		Required: []string{
			"read-only root, no writable volumes, non-root identity, no capabilities or privilege escalation",
			"verified network enforcement restricting the agent to its dedicated gateway; no agent DNS exception",
			"operator-owned gateway configuration and explicit peer/skill endpoint bindings",
			"trusted pod labels, admission and exec/debug access; process-tree inheritance is not an admin boundary",
		},
	}, nil
}
