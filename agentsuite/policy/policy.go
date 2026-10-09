// Package policy contains experimental, provider-neutral AgentSuite policy data.
// It is not a runtime authorizer or an enforcement implementation.
package policy

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

const Version = "agentsuite.dev/policy/v1alpha1"

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var hostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

// Document describes one agent's requested restrictions, not an operator grant.
// An operator must approve the exact digest and destination before activation.
type Document struct {
	APIVersion   string       `json:"apiVersion"`
	Agent        string       `json:"agent"`
	Capabilities Capabilities `json:"capabilities"`
	Filesystem   Filesystem   `json:"filesystem"`
	Network      Network      `json:"network"`
	Invocations  Invocations  `json:"invocations"`
}

// Capabilities is advertisement input, never permission to invoke a skill.
// The runtime must bind endpoint, identity and authentication to form an A2A card.
type Capabilities struct {
	Protocol string  `json:"protocol"`
	Version  string  `json:"version"`
	Skills   []Skill `json:"skills"`
}

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type Filesystem struct {
	Write string `json:"write"`
}

type Network struct {
	Default string        `json:"default"`
	Allow   []Destination `json:"allow"`
}

// Destination is an exact authority, not a URL, wildcard or resolved IP.
// TLS verification and DNS/IP binding are obligations of a future translator.
type Destination struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

// Invocations restricts outbound calls to peer agents, not incoming user access.
type Invocations struct {
	Default string       `json:"default"`
	Allow   []Invocation `json:"allow"`
}

type Invocation struct {
	Agent  string   `json:"agent"`
	Skills []string `json:"skills"`
}

func (d Document) Validate() error {
	if d.APIVersion != Version || !identifier.MatchString(d.Agent) {
		return errors.New("policy requires the experimental apiVersion and a valid agent ID")
	}
	if d.Filesystem.Write != "deny" || d.Network.Default != "deny" || d.Invocations.Default != "deny" {
		return errors.New("experimental policy requires explicit filesystem write deny and network/invocation default deny")
	}
	if d.Network.Allow == nil || d.Invocations.Allow == nil || d.Capabilities.Skills == nil {
		return errors.New("allow and skills arrays must be explicit, including when empty")
	}
	if d.Capabilities.Protocol != "a2a" || d.Capabilities.Version != "1.0.0" {
		return errors.New("experimental capability projection supports A2A 1.0.0 only")
	}
	seen := map[string]bool{}
	for _, skill := range d.Capabilities.Skills {
		if !identifier.MatchString(skill.ID) || strings.TrimSpace(skill.Name) == "" ||
			strings.TrimSpace(skill.Description) == "" || skill.Tags == nil || seen[skill.ID] {
			return errors.New("advertised skills require unique IDs, names, descriptions and explicit tags")
		}
		seen[skill.ID] = true
	}
	seen = map[string]bool{}
	for _, destination := range d.Network.Allow {
		if (destination.Scheme != "http" && destination.Scheme != "https") ||
			len(destination.Host) > 253 || !hostname.MatchString(destination.Host) ||
			net.ParseIP(destination.Host) != nil || destination.Port < 1 || destination.Port > 65535 {
			return errors.New("network allow requires exact lowercase DNS host, http/https scheme and port; URL paths, IPs and wildcards are unsupported")
		}
		key := fmt.Sprintf("%s://%s:%d", destination.Scheme, destination.Host, destination.Port)
		if seen[key] {
			return errors.New("duplicate network destination")
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, invocation := range d.Invocations.Allow {
		if !identifier.MatchString(invocation.Agent) || invocation.Agent == d.Agent ||
			seen[invocation.Agent] || len(invocation.Skills) == 0 {
			return errors.New("invocation grants require unique non-self agent IDs and explicit skill IDs")
		}
		seen[invocation.Agent] = true
		skills := map[string]bool{}
		for _, skill := range invocation.Skills {
			if !identifier.MatchString(skill) || skills[skill] {
				return errors.New("invocation skill IDs must be valid and unique")
			}
			skills[skill] = true
		}
	}
	return nil
}
