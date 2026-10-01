package runtime

import (
	"fmt"
	"strings"
)

type SandboxBackend string

const (
	SandboxHyperlightJS SandboxBackend = "hyperlight-js"
	SandboxUnikraft     SandboxBackend = "unikraft"
	SandboxPod          SandboxBackend = "pod"
)

// SandboxRequirements are workload facts, not platform preferences. KMX uses
// them to resolve the smallest compatible sandbox and records both the facts
// and the resolved backend in the portable revision.
type SandboxRequirements struct {
	Language       string `yaml:"language,omitempty" json:"language,omitempty"`
	Shell          bool   `yaml:"shell,omitempty" json:"shell,omitempty"`
	NativePackages bool   `yaml:"nativePackages,omitempty" json:"nativePackages,omitempty"`
	ContainerImage bool   `yaml:"containerImage,omitempty" json:"containerImage,omitempty"`
	DeviceAccess   bool   `yaml:"deviceAccess,omitempty" json:"deviceAccess,omitempty"`
}

type SandboxSpec struct {
	Backend      SandboxBackend      `yaml:"backend" json:"backend"`
	Requirements SandboxRequirements `yaml:"requirements" json:"requirements"`
}

type SandboxPlan struct {
	Spec   SandboxSpec
	Reason string
}

func SelectSandbox(requested string, requirements SandboxRequirements) (*SandboxPlan, error) {
	requirements.Language = normalizeSandboxLanguage(requirements.Language)
	requested = strings.TrimSpace(requested)
	if requested == "" && requirements.empty() {
		return nil, nil
	}
	if requested == "" {
		requested = "auto"
	}
	if err := requirements.validate(); err != nil {
		return nil, err
	}

	selected, reason := SandboxBackend(requested), "explicit sandbox selection"
	if requested == "auto" {
		switch {
		case requirements.ContainerImage || requirements.DeviceAccess:
			selected = SandboxPod
			reason = "container image or device access requires a full pod sandbox"
		case requirements.Shell || requirements.NativePackages || requirements.Language != "javascript":
			selected = SandboxUnikraft
			reason = "Linux ABI, shell, package, or non-JavaScript requirements need a Unikraft guest"
		default:
			selected = SandboxHyperlightJS
			reason = "JavaScript-only execution fits the smallest hardware-isolated runtime"
		}
	}
	spec := SandboxSpec{Backend: selected, Requirements: requirements}
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return &SandboxPlan{Spec: spec, Reason: reason}, nil
}

func normalizeSandboxLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "js":
		return "javascript"
	default:
		return strings.ToLower(strings.TrimSpace(language))
	}
}

func (r SandboxRequirements) empty() bool {
	return r.Language == "" && !r.Shell && !r.NativePackages && !r.ContainerImage && !r.DeviceAccess
}

func (r SandboxRequirements) validate() error {
	if r.empty() {
		return fmt.Errorf("sandbox selection requires at least one workload requirement, such as --sandbox-language javascript")
	}
	if strings.ContainsAny(r.Language, "\r\n\t") {
		return fmt.Errorf("sandbox language must be a single token")
	}
	return nil
}

func (s SandboxSpec) validate() error {
	if err := s.Requirements.validate(); err != nil {
		return fmt.Errorf("spec.sandbox.requirements: %w", err)
	}
	switch s.Backend {
	case SandboxHyperlightJS:
		if s.Requirements.Language != "javascript" || s.Requirements.Shell || s.Requirements.NativePackages ||
			s.Requirements.ContainerImage || s.Requirements.DeviceAccess {
			return fmt.Errorf("sandbox %q supports JavaScript-only workloads without a shell, native packages, container image, or device access", s.Backend)
		}
	case SandboxUnikraft:
		if s.Requirements.ContainerImage || s.Requirements.DeviceAccess {
			return fmt.Errorf("sandbox %q cannot satisfy a container image or device access requirement; use %q", s.Backend, SandboxPod)
		}
	case SandboxPod:
		// A pod is the compatibility ceiling for this POC.
	default:
		return fmt.Errorf("sandbox backend must be auto, %q, %q, or %q", SandboxHyperlightJS, SandboxUnikraft, SandboxPod)
	}
	return nil
}
