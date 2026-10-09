// Package governance translates experimental policy requests into explicit
// enforcement artifacts. Compilation is not activation or proof of enforcement.
package governance

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gowebpki/jcs"
	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
)

const Backend = "linux-python-landlock-deny-v1"

//go:embed launcher.py
var launcher string

type Plan struct {
	Backend       string        `json:"backend"`
	PolicyDigest  string        `json:"policyDigest"`
	ProfileDigest string        `json:"profileDigest"`
	ProfilePath   string        `json:"profilePath"`
	NodeLabel     string        `json:"nodeLabel"`
	Seccomp       Seccomp       `json:"seccomp"`
	Gateway       GatewayConfig `json:"gateway"`
	Coverage      []string      `json:"coverage"`
	Launcher      string        `json:"launcher"`
}

type Seccomp struct {
	DefaultAction string        `json:"defaultAction"`
	Architectures []string      `json:"architectures"`
	Syscalls      []SyscallRule `json:"syscalls"`
}

type SyscallRule struct {
	Names    []string   `json:"names"`
	Action   string     `json:"action"`
	ErrnoRet uint       `json:"errnoRet"`
	Args     []Argument `json:"args,omitempty"`
}

type Argument struct {
	Index uint   `json:"index"`
	Value uint64 `json:"value"`
	Op    string `json:"op"`
}

// GatewayConfig is scoped to one dedicated listener and fixed local test
// backend. It is not a general forward proxy or an authenticated A2A grant.
type GatewayConfig struct {
	Gateways map[string]Gateway `json:"gateways"`
	Routes   []Route            `json:"routes"`
}
type Gateway struct {
	Port int `json:"port"`
}
type Route struct {
	Policies RoutePolicies `json:"policies"`
	Backends []HostBackend `json:"backends"`
}
type RoutePolicies struct {
	Authorization Authorization `json:"authorization"`
}
type Authorization struct {
	Rules []RequireRule `json:"rules"`
}
type RequireRule struct {
	Require string `json:"require"`
}
type HostBackend struct {
	Host string `json:"host"`
}

func Compile(document policy.Document, backend string) (Plan, error) {
	if err := document.Validate(); err != nil {
		return Plan{}, err
	}
	if backend != Backend {
		return Plan{}, fmt.Errorf("unsupported required policy backend %q", backend)
	}
	if len(document.Network.Allow) != 0 || len(document.Invocations.Allow) != 0 {
		return Plan{}, errors.New("linux-python-landlock-deny-v1 cannot enforce hostname/port allowlists or skill grants; refusing rather than weakening policy")
	}
	digest, err := Digest(document)
	if err != nil {
		return Plan{}, err
	}
	profile := denyProfile()
	profileDigest, err := Digest(profile)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Backend: Backend, PolicyDigest: digest, ProfileDigest: profileDigest,
		ProfilePath: "agentsuite/" + profileDigest[7:] + ".json",
		NodeLabel:   "agentsuite.dev/profile-" + profileDigest[7:39],
		Seccomp:     profile,
		Launcher:    launcher,
		Gateway: GatewayConfig{
			Gateways: map[string]Gateway{"default": {Port: 3000}},
			Routes: []Route{{
				Policies: RoutePolicies{Authorization: Authorization{Rules: []RequireRule{{Require: "false"}}}},
				Backends: []HostBackend{{Host: "127.0.0.1:8080"}},
			}},
		},
		Coverage: []string{
			"linux-amd64 processes: deny new outbound connections and datagrams, including DNS and Unix sockets",
			"Landlock ABI >= 3 launcher plus read-only mounts and seccomp: deny filesystem writes; stdout/stderr and accepted-socket replies remain usable",
			"dedicated agentgateway listener: require false, deny every routed HTTP request",
			"no native Orka/kagent, authenticated A2A skill allowlist, Azure sandbox or gVisor conformance claim",
		},
	}, nil
}

func Digest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	data, err = jcs.Transform(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

func denyProfile() Seccomp {
	profile := Seccomp{DefaultAction: "SCMP_ACT_ALLOW", Architectures: []string{"SCMP_ARCH_X86_64", "SCMP_ARCH_X86", "SCMP_ARCH_X32"}}
	profile.Syscalls = append(profile.Syscalls, SyscallRule{
		Action: "SCMP_ACT_ERRNO", ErrnoRet: 1,
		Names: []string{
			"connect", "sendmsg", "sendmmsg", "socketcall",
			"creat", "openat2", "truncate", "ftruncate", "truncate64", "ftruncate64",
			"rename", "renameat", "renameat2", "unlink", "unlinkat", "mkdir", "mkdirat", "rmdir",
			"link", "linkat", "symlink", "symlinkat", "mknod", "mknodat",
			"chmod", "fchmod", "fchmodat", "fchmodat2", "chown", "fchown", "lchown", "fchownat",
			"setxattr", "lsetxattr", "fsetxattr", "removexattr", "lremovexattr", "fremovexattr",
			"utime", "utimes", "futimesat", "utimensat", "fallocate",
			"io_uring_setup", "io_uring_enter", "io_uring_register",
			"mount", "umount2", "pivot_root", "move_mount", "open_tree", "fsopen", "fsmount", "fspick", "mount_setattr",
			"ptrace", "process_vm_writev", "pidfd_getfd", "bpf", "userfaultfd", "open_by_handle_at",
		},
	})
	// send()/sendall() use sendto with a NULL destination on accepted sockets.
	// Permit replies, but deny every explicitly addressed datagram. connect is
	// denied separately, so an agent cannot establish a connected UDP socket.
	profile.Syscalls = append(profile.Syscalls, SyscallRule{
		Names: []string{"sendto"}, Action: "SCMP_ACT_ERRNO", ErrnoRet: 1,
		Args: []Argument{{Index: 4, Value: 0, Op: "SCMP_CMP_NE"}},
	})
	return profile
}
