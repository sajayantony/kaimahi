package policy

import (
	"errors"
	"fmt"
	"slices"
)

const SuiteVersion = "agentsuite.dev/suite-policy/v1alpha1"

// SuitePolicy uses logical resource and agent identities. Deployment selectors,
// service addresses and implementation-specific policy objects do not belong here.
type SuitePolicy struct {
	APIVersion string        `json:"apiVersion"`
	Suite      string        `json:"suite"`
	Defaults   SuiteDefaults `json:"defaults"`
	Resources  []Resource    `json:"resources"`
	Agents     []AgentPolicy `json:"agents"`
}

type SuiteDefaults struct {
	FilesystemWrite string `json:"filesystemWrite"`
	Network         string `json:"network"`
	Invocations     string `json:"invocations"`
}

type Resource struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Destination *Destination `json:"destination,omitempty"`
}

type AgentPolicy struct {
	Agent        string       `json:"agent"`
	Capabilities Capabilities `json:"capabilities"`
	Resources    []string     `json:"resources"`
	Invocations  []Invocation `json:"invocations"`
}

func (p SuitePolicy) Validate() error {
	if p.APIVersion != SuiteVersion || !identifier.MatchString(p.Suite) ||
		p.Defaults != (SuiteDefaults{"deny", "deny", "deny"}) || p.Resources == nil || len(p.Agents) == 0 {
		return errors.New("suite policy requires a valid identity, explicit resources/agents and deny defaults")
	}
	resources := map[string]bool{}
	for _, r := range p.Resources {
		if !identifier.MatchString(r.ID) || resources[r.ID] {
			return errors.New("resource identities must be valid and unique")
		}
		resources[r.ID] = true
		if r.Kind != "model" && r.Kind != "https" || (r.Kind == "model") != (r.Destination == nil) {
			return errors.New("resources support logical models or explicit HTTPS authorities only")
		}
		if r.Destination != nil {
			d := Document{APIVersion: Version, Agent: "resource", Capabilities: Capabilities{"a2a", "1.0.0", []Skill{}},
				Filesystem: Filesystem{"deny"}, Network: Network{"deny", []Destination{*r.Destination}}, Invocations: Invocations{"deny", []Invocation{}}}
			if err := d.Validate(); err != nil || r.Destination.Scheme != "https" {
				return fmt.Errorf("resource %s requires an exact HTTPS authority", r.ID)
			}
		}
	}
	agents := map[string]AgentPolicy{}
	for _, a := range p.Agents {
		if _, exists := agents[a.Agent]; exists {
			return errors.New("duplicate policy agent")
		}
		d := Document{APIVersion: Version, Agent: a.Agent, Capabilities: a.Capabilities,
			Filesystem: Filesystem{"deny"}, Network: Network{"deny", []Destination{}}, Invocations: Invocations{"deny", a.Invocations}}
		if err := d.Validate(); err != nil {
			return err
		}
		if a.Resources == nil {
			return errors.New("agent resources must be explicit, including when empty")
		}
		seen := map[string]bool{}
		for _, resource := range a.Resources {
			if !resources[resource] || seen[resource] {
				return errors.New("agent resource requests must reference unique declared resources")
			}
			seen[resource] = true
		}
		agents[a.Agent] = a
	}
	for _, a := range p.Agents {
		for _, call := range a.Invocations {
			peer, ok := agents[call.Agent]
			if !ok {
				return errors.New("invocation references an undeclared agent")
			}
			for _, skill := range call.Skills {
				if !slices.ContainsFunc(peer.Capabilities.Skills, func(s Skill) bool { return s.ID == skill }) {
					return fmt.Errorf("peer %s does not advertise skill %s", peer.Agent, skill)
				}
			}
		}
	}
	return nil
}
