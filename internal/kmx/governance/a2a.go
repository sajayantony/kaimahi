package governance

import "github.com/kaimahi-agents/kaimahi/agentsuite/policy"

// AgentCard is advertisement metadata for the example's REST-shaped interface.
// Neither an advertised skill nor a URL grants permission to call it.
type AgentCard struct {
	Name                string           `json:"name"`
	Description         string           `json:"description"`
	Version             string           `json:"version"`
	SupportedInterfaces []AgentInterface `json:"supportedInterfaces"`
	Capabilities        struct{}         `json:"capabilities"`
	DefaultInputModes   []string         `json:"defaultInputModes"`
	DefaultOutputModes  []string         `json:"defaultOutputModes"`
	Skills              []policy.Skill   `json:"skills"`
}
type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
}

func projectAgentCard(a resolvedAgent, namespace string) AgentCard {
	return AgentCard{Name: a.Policy.Agent, Description: a.Policy.Capabilities.Skills[0].Description, Version: "0.1.0",
		SupportedInterfaces: []AgentInterface{{URL: "http://" + a.Binding.Workload + "." + namespace + ".svc.cluster.local:8080/skills/" + a.Policy.Capabilities.Skills[0].ID, ProtocolBinding: "HTTP+JSON", ProtocolVersion: "1.0"}},
		DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"}, Skills: a.Policy.Capabilities.Skills}
}
