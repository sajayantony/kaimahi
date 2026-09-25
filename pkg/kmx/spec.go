package kmx

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ContractVersion string

const ContractV1Alpha1 ContractVersion = "kmx.kaimahi.dev/v1alpha1"

type Capability string

const (
	CapabilitySubmit    Capability = "submit"
	CapabilityStatus    Capability = "status"
	CapabilityEvents    Capability = "events"
	CapabilityLogs      Capability = "logs"
	CapabilityOutput    Capability = "output"
	CapabilityArtifacts Capability = "artifacts"
	CapabilityCancel    Capability = "cancel"
	CapabilityDelete    Capability = "delete"
)

type WorkloadSpec struct {
	APIVersion   ContractVersion      `json:"apiVersion"`
	Kind         string               `json:"kind"`
	Name         string               `json:"name"`
	Agent        AgentSpec            `json:"agent"`
	Requirements RequirementSet       `json:"requirements,omitempty"`
	Target       TargetSelector       `json:"target"`
	Inputs       map[string]string    `json:"inputs,omitempty"`
	Secrets      map[string]SecretRef `json:"secrets,omitempty"`
}

type AgentSpec struct {
	Instructions string   `json:"instructions"`
	Model        ModelRef `json:"model"`
	Tools        []string `json:"tools,omitempty"`
}

type ModelRef struct {
	Name string `json:"name"`
}

type RequirementSet struct {
	Capabilities []Capability `json:"capabilities,omitempty"`
	Constraints  []Constraint `json:"constraints,omitempty"`
}

type Constraint struct {
	Name     string   `json:"name"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

type TargetSelector struct {
	Class string            `json:"class"`
	Match map[string]string `json:"match,omitempty"`
}

type TargetRef struct {
	ID    string `json:"id"`
	Class string `json:"class"`
	Scope string `json:"scope,omitempty"`
}

type SecretRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func (s WorkloadSpec) Validate() error {
	if s.APIVersion != ContractV1Alpha1 {
		return &InvalidError{Field: "apiVersion", Reason: "unsupported contract version"}
	}
	if strings.TrimSpace(s.Kind) != "Workload" {
		return &InvalidError{Field: "kind", Reason: "must be Workload"}
	}
	if strings.TrimSpace(s.Name) == "" {
		return &InvalidError{Field: "name", Reason: "must not be empty"}
	}
	if strings.TrimSpace(s.Agent.Instructions) == "" {
		return &InvalidError{Field: "agent.instructions", Reason: "must not be empty"}
	}
	if strings.TrimSpace(s.Agent.Model.Name) == "" {
		return &InvalidError{Field: "agent.model.name", Reason: "must not be empty"}
	}
	if strings.TrimSpace(s.Target.Class) == "" {
		return &InvalidError{Field: "target.class", Reason: "must not be empty"}
	}
	for i, capability := range s.Requirements.Capabilities {
		if strings.TrimSpace(string(capability)) == "" {
			return &InvalidError{
				Field:  fmt.Sprintf("requirements.capabilities[%d]", i),
				Reason: "must not be empty",
			}
		}
	}
	for i, constraint := range s.Requirements.Constraints {
		if strings.TrimSpace(constraint.Name) == "" {
			return &InvalidError{
				Field:  fmt.Sprintf("requirements.constraints[%d].name", i),
				Reason: "must not be empty",
			}
		}
		switch strings.ToLower(strings.TrimSpace(constraint.Name)) {
		case "provider", "implementation", "adapter", "backend":
			return &InvalidError{
				Field:  fmt.Sprintf("requirements.constraints[%d].name", i),
				Reason: "must describe a portable workload property",
			}
		}
		switch constraint.Operator {
		case "equals":
			if len(constraint.Values) != 1 {
				return &InvalidError{
					Field:  fmt.Sprintf("requirements.constraints[%d].values", i),
					Reason: "equals requires exactly one value",
				}
			}
		case "in":
			if len(constraint.Values) == 0 {
				return &InvalidError{
					Field:  fmt.Sprintf("requirements.constraints[%d].values", i),
					Reason: "in requires at least one value",
				}
			}
		default:
			return &InvalidError{
				Field:  fmt.Sprintf("requirements.constraints[%d].operator", i),
				Reason: "must be equals or in",
			}
		}
	}
	return nil
}

func (s WorkloadSpec) Digest() (Digest, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return Digest{}, fmt.Errorf("marshal workload: %w", err)
	}
	return NewDigest(data), nil
}
