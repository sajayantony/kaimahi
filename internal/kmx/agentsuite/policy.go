package agentsuite

import "github.com/kaimahi-agents/kaimahi/agentsuite/policy"

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
