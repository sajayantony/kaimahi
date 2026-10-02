package agentsuite

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
	"go.yaml.in/yaml/v3"
)

const (
	APIVersion = "kmx.kaimahi.dev/v1alpha1"
	Kind       = "AgentSuite"

	ArtifactType    = "application/vnd.kaimahi.agent-suite.v1"
	ConfigMediaType = "application/vnd.kaimahi.agent-suite.config.v1+json"
	SuiteMediaType  = "application/vnd.kaimahi.agent-suite.source.v1+yaml"
	AgentMediaType  = "application/vnd.kaimahi.portable-agent.v1+yaml"
)

type Suite struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type Spec struct {
	Entrypoint   string       `yaml:"entrypoint"`
	Agents       []Member     `yaml:"agents"`
	Aggregation  Aggregation  `yaml:"aggregation"`
	Requirements Requirements `yaml:"requirements"`
}

type Member struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
	Role   string `yaml:"role"`
}

type Aggregation struct {
	Member   string   `yaml:"member"`
	Strategy string   `yaml:"strategy"`
	Inputs   []string `yaml:"inputs"`
}

type Requirements struct {
	Runtime      string   `yaml:"runtime"`
	Capabilities []string `yaml:"capabilities"`
}

type Resolved struct {
	APIVersion   string              `json:"apiVersion"`
	Kind         string              `json:"kind"`
	Name         string              `json:"name"`
	Entrypoint   string              `json:"entrypoint"`
	Runtime      string              `json:"runtime"`
	Capabilities []string            `json:"capabilities"`
	Aggregation  ResolvedAggregation `json:"aggregation"`
	Members      []ResolvedMember    `json:"members"`

	source []byte
}

type ResolvedAggregation struct {
	Member   string   `json:"member"`
	Strategy string   `json:"strategy"`
	Inputs   []string `json:"inputs"`
}

type ResolvedMember struct {
	Name           string `json:"name"`
	Role           string `json:"role"`
	Source         string `json:"source"`
	PortableDigest string `json:"portableDigest"`
	Model          string `json:"model"`

	source []byte
	agent  *agentruntime.PortableAgent
}

func (r *Resolved) Digest() (string, error) {
	body, err := r.Config()
	if err != nil {
		return "", err
	}
	return digest(body), nil
}

func (r *Resolved) Config() ([]byte, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode resolved suite: %w", err)
	}
	return append(body, '\n'), nil
}

func (r *Resolved) Source() []byte {
	return append([]byte(nil), r.source...)
}

func (m ResolvedMember) SourceBytes() []byte {
	return append([]byte(nil), m.source...)
}

func (m ResolvedMember) Agent() *agentruntime.PortableAgent {
	return m.agent
}

func (r *Resolved) Member(name string) (ResolvedMember, bool) {
	for _, member := range r.Members {
		if member.Name == name {
			return member, true
		}
	}
	return ResolvedMember{}, false
}

func Load(path string) (*Resolved, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve suite path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("read suite directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("suite path must be a directory")
	}
	suitePath := filepath.Join(root, "suite.yaml")
	source, err := readRegularFile(suitePath, "suite.yaml")
	if err != nil {
		return nil, err
	}
	suite, err := parse(source)
	if err != nil {
		return nil, err
	}

	resolved := &Resolved{
		APIVersion:   APIVersion,
		Kind:         "ResolvedAgentSuite",
		Name:         suite.Metadata.Name,
		Entrypoint:   suite.Spec.Entrypoint,
		Runtime:      suite.Spec.Requirements.Runtime,
		Capabilities: append([]string(nil), suite.Spec.Requirements.Capabilities...),
		Aggregation: ResolvedAggregation{
			Member:   suite.Spec.Aggregation.Member,
			Strategy: suite.Spec.Aggregation.Strategy,
			Inputs:   append([]string(nil), suite.Spec.Aggregation.Inputs...),
		},
		source: source,
	}

	for _, declared := range suite.Spec.Agents {
		bundle, err := resolveMemberBundle(root, declared.Source)
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", declared.Name, err)
		}
		agentSource, err := readRegularFile(filepath.Join(bundle, "agent.yaml"), "agent.yaml")
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", declared.Name, err)
		}
		agent, err := agentruntime.ParsePortableAgent(agentSource)
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", declared.Name, err)
		}
		if agent.Metadata.Name != declared.Name {
			return nil, fmt.Errorf("agent %s source declares metadata.name %q", declared.Name, agent.Metadata.Name)
		}
		if err := validatePortableBehavior(agent); err != nil {
			return nil, fmt.Errorf("agent %s: %w", declared.Name, err)
		}
		resolved.Members = append(resolved.Members, ResolvedMember{
			Name:           declared.Name,
			Role:           declared.Role,
			Source:         filepath.ToSlash(declared.Source),
			PortableDigest: "sha256:" + agentruntime.PortableBundleDigest(agentSource),
			Model:          agent.Spec.Model.Name,
			source:         agentSource,
			agent:          agent,
		})
	}
	if err := validateResolvedGraph(resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func parse(source []byte) (*Suite, error) {
	var suite Suite
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	decoder.KnownFields(true)
	if err := decoder.Decode(&suite); err != nil {
		return nil, fmt.Errorf("invalid suite.yaml: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("suite.yaml must contain exactly one YAML document")
	}
	if suite.APIVersion != APIVersion || suite.Kind != Kind {
		return nil, fmt.Errorf("suite.yaml must be %s %s", APIVersion, Kind)
	}
	if err := scaffold.ValidateName(suite.Metadata.Name); err != nil {
		return nil, fmt.Errorf("invalid suite metadata.name: %w", err)
	}
	if err := scaffold.ValidateName(suite.Spec.Entrypoint); err != nil {
		return nil, fmt.Errorf("invalid suite entrypoint: %w", err)
	}
	if len(suite.Spec.Agents) < 2 {
		return nil, fmt.Errorf("suite requires at least two agents")
	}
	seen := map[string]bool{}
	for i, member := range suite.Spec.Agents {
		if err := scaffold.ValidateName(member.Name); err != nil {
			return nil, fmt.Errorf("invalid spec.agents[%d].name: %w", i, err)
		}
		if seen[member.Name] {
			return nil, fmt.Errorf("duplicate suite member %q", member.Name)
		}
		seen[member.Name] = true
		if member.Source == "" || filepath.IsAbs(member.Source) {
			return nil, fmt.Errorf("agent %s source must be a relative bundle path", member.Name)
		}
		if member.Role != "coordinator" && member.Role != "specialist" {
			return nil, fmt.Errorf("agent %s role must be coordinator or specialist", member.Name)
		}
	}
	if !seen[suite.Spec.Entrypoint] {
		return nil, fmt.Errorf("entrypoint %q is not a suite member", suite.Spec.Entrypoint)
	}
	if suite.Spec.Aggregation.Member != suite.Spec.Entrypoint {
		return nil, fmt.Errorf("POC aggregation member must equal entrypoint %q", suite.Spec.Entrypoint)
	}
	if suite.Spec.Aggregation.Strategy != "synthesize" {
		return nil, fmt.Errorf("POC aggregation strategy must be synthesize")
	}
	if len(suite.Spec.Aggregation.Inputs) == 0 {
		return nil, fmt.Errorf("aggregation requires at least one input")
	}
	inputs := map[string]bool{}
	for _, name := range suite.Spec.Aggregation.Inputs {
		if !seen[name] {
			return nil, fmt.Errorf("aggregation input %q is not a suite member", name)
		}
		if name == suite.Spec.Aggregation.Member {
			return nil, fmt.Errorf("aggregation member cannot consume itself")
		}
		if inputs[name] {
			return nil, fmt.Errorf("duplicate aggregation input %q", name)
		}
		inputs[name] = true
	}
	if suite.Spec.Requirements.Runtime != "agentsessions-substrate" {
		return nil, fmt.Errorf("POC runtime must be agentsessions-substrate")
	}
	required := []string{"durableSessions", "statelessReplay"}
	for _, capability := range required {
		if !slices.Contains(suite.Spec.Requirements.Capabilities, capability) {
			return nil, fmt.Errorf("suite requires capability %q", capability)
		}
	}
	return &suite, nil
}

func validateResolvedGraph(suite *Resolved) error {
	entrypoint, ok := suite.Member(suite.Entrypoint)
	if !ok {
		return fmt.Errorf("entrypoint %q was not resolved", suite.Entrypoint)
	}
	if entrypoint.Role != "coordinator" {
		return fmt.Errorf("entrypoint %q must have role coordinator", suite.Entrypoint)
	}
	allowed := map[string]bool{}
	if coordination := entrypoint.Agent().Spec.Coordination; coordination != nil {
		for _, ref := range coordination.AllowedAgents {
			allowed[ref.Name] = true
		}
	}
	for _, input := range suite.Aggregation.Inputs {
		if !allowed[input] {
			return fmt.Errorf("entrypoint %q does not allow aggregation input %q", suite.Entrypoint, input)
		}
		member, _ := suite.Member(input)
		if member.Role != "specialist" {
			return fmt.Errorf("aggregation input %q must have role specialist", input)
		}
	}
	for name := range allowed {
		if !slices.Contains(suite.Aggregation.Inputs, name) {
			return fmt.Errorf("entrypoint allows %q but the suite does not declare it as an aggregation input", name)
		}
	}
	return nil
}

func validatePortableBehavior(agent *agentruntime.PortableAgent) error {
	if agent.Extensions.Kagent != nil {
		return fmt.Errorf("Kagent extensions are not supported by the AgentSessions suite POC")
	}
	if extension := agent.Extensions.Orka; extension != nil {
		if extension.Provider.RateLimit != nil {
			return fmt.Errorf("AgentSessions cannot honor extensions.orka.provider.rateLimit")
		}
		if extension.Agent != nil && (len(extension.Agent.Tools) != 0 ||
			len(extension.Agent.Skills) != 0 ||
			extension.Agent.RateLimit != nil ||
			extension.Agent.Coordination != nil) {
			return fmt.Errorf("AgentSessions cannot honor Orka-only agent behavior")
		}
	}
	return nil
}

func resolveMemberBundle(root, source string) (string, error) {
	clean := filepath.Clean(source)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("source must remain inside the suite directory")
	}
	path := filepath.Join(root, clean)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve suite directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve source: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("source must remain inside the suite directory")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("read source bundle: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source bundle must be a directory")
	}
	return resolved, nil
}

func readRegularFile(path, label string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	return body, nil
}

func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
