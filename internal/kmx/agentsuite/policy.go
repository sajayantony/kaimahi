package agentsuite

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
)

// DecodePolicy reuses the existing strict AgentSuite JSON boundary. Policy is
// deliberately a standalone experimental document, not a silently ignored extension.
func DecodePolicy(data []byte) (policy.Document, error) {
	var document policy.Document
	if err := decodeStrict(data, &document); err != nil {
		return policy.Document{}, err
	}
	if err := document.Validate(); err != nil {
		return policy.Document{}, err
	}
	return document, nil
}

func DecodeSuitePolicy(data []byte) (policy.SuitePolicy, error) {
	var document policy.SuitePolicy
	if err := decodeStrict(data, &document); err != nil {
		return policy.SuitePolicy{}, err
	}
	var fields struct {
		Resources []map[string]json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return policy.SuitePolicy{}, err
	}
	for _, resource := range fields.Resources {
		if value, present := resource["destination"]; present && string(value) == "null" {
			return policy.SuitePolicy{}, errors.New("resource destination must be omitted or contain an HTTPS authority, not null")
		}
	}
	if err := document.Validate(); err != nil {
		return policy.SuitePolicy{}, err
	}
	return document, nil
}

// DecodeStrictJSON shares the AgentSuite boundary with experimental binding
// adapters: duplicate keys, wrong field casing and unknown fields fail.
func DecodeStrictJSON(data []byte, target any) error { return decodeStrict(data, target) }

func (v *validator) loadSuitePolicy() error {
	if v.suite.Policy == nil {
		return nil
	}
	ref := v.suite.Policy
	if _, err := validateContentPath(ref.Path); err != nil || !validDigest(ref.Digest) {
		return errors.New("invalid suite policy reference")
	}
	raw, err := v.content.data(ref.Path)
	if err != nil {
		return err
	}
	digest, err := canonicalDigest(raw)
	if err != nil || digest != ref.Digest {
		return errors.New("suite policy digest mismatch")
	}
	p, err := DecodeSuitePolicy(raw)
	if err != nil {
		return err
	}
	if err := validatePolicyMembers(p, v.suite.Name, v.agents); err != nil {
		return err
	}
	v.policy = &p
	return nil
}

func validatePolicyMembers(p policy.SuitePolicy, name string, agents map[string]Agent) error {
	if p.Suite != name || len(p.Agents) != len(agents) {
		return errors.New("suite policy identity and membership must exactly match the suite")
	}

	for _, a := range p.Agents {
		member, ok := agents[a.Agent]
		if !ok {
			return fmt.Errorf("policy agent %s is not in the suite", a.Agent)
		}
		// Declared invocation requirements cannot be silently removed or
		// augmented by the policy attached to the same suite.
		required := map[string]bool{}
		for _, edge := range member.Invokes {
			required[edge.Agent] = true
		}
		if len(required) != len(a.Invocations) {
			return fmt.Errorf("agent %s invocation requirements disagree with policy", a.Agent)
		}
		for _, edge := range a.Invocations {
			if !required[edge.Agent] {
				return fmt.Errorf("agent %s policy adds undeclared peer %s", a.Agent, edge.Agent)
			}
		}
	}
	return nil
}

func (s *DeploymentSuite) CheckPolicy(p policy.SuitePolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if s.suite.Policy != nil {
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		digest, err := canonicalDigest(raw)
		if err != nil {
			return err
		}
		if digest != s.suite.Policy.Digest {
			return errors.New("policy conflicts with bundled reference")
		}
	}
	agents := map[string]Agent{}
	for _, member := range s.members {
		agents[member.Agent.ID] = member.Agent
	}
	return validatePolicyMembers(p, s.Name(), agents)
}

// ResolvePolicyDeploymentSuite is the explicit policy-aware path. Supplying an
// external policy cannot override different bundled bytes; both identities must
// agree. The returned policy is a request, not operator approval.
func ResolvePolicyDeploymentSuite(root string, platform Platform, external []byte) (*DeploymentSuite, policy.SuitePolicy, error) {
	suite, err := resolveDeploymentSuite(root, platform, true)
	if err != nil {
		return nil, policy.SuitePolicy{}, err
	}
	selected := suite.policy
	if external != nil {
		p, err := DecodeSuitePolicy(external)
		if err != nil {
			return nil, policy.SuitePolicy{}, err
		}
		digest, err := canonicalDigest(external)
		if err != nil {
			return nil, policy.SuitePolicy{}, err
		}
		if suite.suite.Policy != nil && suite.suite.Policy.Digest != digest {
			return nil, policy.SuitePolicy{}, errors.New("external policy conflicts with bundled policy")
		}
		selected = &p
	}
	if selected == nil {
		return nil, policy.SuitePolicy{}, errors.New("an explicit bundled or external suite policy is required")
	}
	if err := suite.CheckPolicy(*selected); err != nil {
		return nil, policy.SuitePolicy{}, err
	}
	return suite, *selected, nil
}
