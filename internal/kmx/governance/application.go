package governance

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
)

//go:embed cpu_agent.py
var cpuAgent string

type Object = map[string]any

// ApplicationBinding is operator-owned deployment metadata, not portable policy.
type ApplicationBinding struct {
	APIVersion   string                  `json:"apiVersion"`
	SuiteDigest  string                  `json:"suiteDigest"`
	PolicyDigest string                  `json:"policyDigest"`
	Namespace    string                  `json:"namespace"`
	Adapter      string                  `json:"adapter"`
	GatewayImage string                  `json:"gatewayImage"`
	Agents       map[string]AgentBinding `json:"agents"`
	Model        ModelBinding            `json:"model"`
}

type AgentBinding struct {
	Workload string `json:"workload"`
	Runtime  string `json:"runtime"`
	Image    string `json:"image"`
}

type ModelBinding struct {
	Resource string `json:"resource"`
	Workload string `json:"workload"`
	Image    string `json:"image"`
	Model    string `json:"model"`
}

type ApplicationBundle struct {
	Namespace     string                              `json:"namespace"`
	SuiteDigest   string                              `json:"suiteDigest"`
	PolicyDigest  string                              `json:"policyDigest"`
	BindingDigest string                              `json:"bindingDigest"`
	PlanDigest    string                              `json:"planDigest"`
	Adapter       string                              `json:"adapter"`
	Network       []Object                            `json:"network"`
	Services      []Object                            `json:"services"`
	Model         []Object                            `json:"model"`
	Workloads     []Object                            `json:"workloads"`
	Bootstrap     Object                              `json:"bootstrap"`
	Gateways      map[string]Object                   `json:"gateways"`
	Cards         map[string]AgentCard                `json:"cards"`
	Profiles      map[string]GatewaySandboxProjection `json:"profiles"`
	Required      []string                            `json:"required"`
}

type resolvedAgent struct {
	Policy       policy.AgentPolicy
	Binding      AgentBinding
	Peer         string
	Destination  *policy.Destination
	Instructions string
}

var kubeName = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,48}[a-z0-9])?$`)

func CompileApplication(suite *agentsuite.DeploymentSuite, p policy.SuitePolicy, b ApplicationBinding) (ApplicationBundle, error) {
	if err := suite.CheckPolicy(p); err != nil {
		return ApplicationBundle{}, err
	}
	pd, err := Digest(p)
	if err != nil {
		return ApplicationBundle{}, err
	}
	if b.APIVersion != "kaimahi.dev/policy-application/v1alpha1" || b.Adapter != "cilium-cpu-v1" ||
		b.SuiteDigest != suite.LogicalDigest() || b.PolicyDigest != pd || p.Suite != suite.Name() ||
		!kubeName.MatchString(b.Namespace) || len(b.Agents) != len(p.Agents) || len(p.Agents) != 2 {
		return ApplicationBundle{}, errors.New("binding must pin the exact suite/policy and select a supported adapter, namespace and complete membership")
	}
	if err := agentsuite.ValidateImageReference(b.GatewayImage); err != nil {
		return ApplicationBundle{}, err
	}
	if err := agentsuite.ValidateImageReference(b.Model.Image); err != nil {
		return ApplicationBundle{}, err
	}
	names := map[string]bool{"validator": true, "default-deny": true, "model-bootstrap": true}
	if !kubeName.MatchString(b.Model.Workload) || names[b.Model.Workload] || b.Model.Model == "" || len(suite.ToolProviders()) != 0 {
		return ApplicationBundle{}, errors.New("CPU adapter requires one named model and no unimplemented ToolProviders")
	}
	names[b.Model.Workload] = true
	resources := map[string]policy.Resource{}
	models := 0
	for _, resource := range p.Resources {
		resources[resource.ID] = resource
		if resource.Kind == "model" {
			models++
		}
	}
	if models != 1 || resources[b.Model.Resource].Kind != "model" {
		return ApplicationBundle{}, errors.New("model binding must resolve the one logical model resource")
	}
	members := map[string]agentsuite.DeploymentMember{}
	for _, member := range suite.Members() {
		members[member.Agent.ID] = member
	}
	if len(members) != len(p.Agents) {
		return ApplicationBundle{}, errors.New("suite membership differs from policy")
	}
	resolved := map[string]resolvedAgent{}
	roles := map[string]int{}
	for _, a := range p.Agents {
		binding, ok := b.Agents[a.Agent]
		member, memberOK := members[a.Agent]
		if !ok || !memberOK || !kubeName.MatchString(binding.Workload) || names[binding.Workload] || names["gateway-"+binding.Workload] ||
			len(a.Capabilities.Skills) != 1 || member.Agent.Model.Protocol != "openai-compatible" || member.Agent.Model.Model != b.Model.Resource {
			return ApplicationBundle{}, errors.New("CPU binding requires unique workloads, one skill per agent and a matching logical OpenAI-compatible model")
		}
		names[binding.Workload], names["gateway-"+binding.Workload] = true, true
		if err := agentsuite.ValidateImageReference(binding.Image); err != nil {
			return ApplicationBundle{}, err
		}
		if member.Agent.Model.EndpointEnv != "" || len(member.Agent.Model.SecretRefs) != 0 {
			return ApplicationBundle{}, errors.New("CPU adapter does not bind external model endpoints or credentials")
		}
		roles[binding.Runtime]++
		r := resolvedAgent{Policy: a, Binding: binding, Instructions: member.Instructions}
		for _, edge := range member.Agent.Invokes {
			if edge.MaxConcurrent != 1 || edge.MaxDepth != 1 {
				return ApplicationBundle{}, errors.New("CPU adapter supports invocation concurrency/depth of one only")
			}
		}
		hasModel := false
		for _, id := range a.Resources {
			resource := resources[id]
			if resource.Kind == "model" {
				hasModel = true
			} else {
				if r.Destination != nil || resource.Destination.Port != 443 {
					return ApplicationBundle{}, errors.New("CPU reader supports one HTTPS authority on port 443")
				}
				copy := *resource.Destination
				r.Destination = &copy
			}
		}
		if !hasModel {
			return ApplicationBundle{}, errors.New("CPU agents require an explicit model permission")
		}
		switch binding.Runtime {
		case "coordinator":
			if len(a.Invocations) != 1 || len(a.Invocations[0].Skills) != 1 || r.Destination != nil {
				return ApplicationBundle{}, errors.New("coordinator adapter requires one single-skill peer and no direct HTTPS resource")
			}
			r.Peer = a.Invocations[0].Agent
		case "registry-reader":
			if len(a.Invocations) != 0 || r.Destination == nil {
				return ApplicationBundle{}, errors.New("reader adapter requires one HTTPS resource and no peer grants")
			}
		default:
			return ApplicationBundle{}, errors.New("unsupported example agent runtime")
		}
		resolved[a.Agent] = r
	}
	if roles["coordinator"] != 1 || roles["registry-reader"] != 1 {
		return ApplicationBundle{}, errors.New("CPU adapter requires one coordinator and one registry-reader")
	}
	for _, r := range resolved {
		if r.Peer != "" && resolved[r.Peer].Binding.Runtime != "registry-reader" {
			return ApplicationBundle{}, errors.New("coordinator peer must be a bound single-skill reader")
		}
	}
	bd, err := Digest(b)
	if err != nil {
		return ApplicationBundle{}, err
	}
	out := ApplicationBundle{Namespace: b.Namespace, SuiteDigest: suite.LogicalDigest(), PolicyDigest: pd,
		BindingDigest: bd, Adapter: b.Adapter, Gateways: map[string]Object{}, Cards: map[string]AgentCard{},
		Profiles: map[string]GatewaySandboxProjection{}, Network: []Object{}, Services: []Object{}, Model: []Object{}, Workloads: []Object{},
		Required: []string{
			"install and verify Cilium, DNS proxy and content-addressed node seccomp profiles before starting agents",
			"bootstrap the CPU model, remove its download policy, then start agent/gateway workloads",
			"operator-controlled labels, namespace, image bindings and exec/debug access are trusted",
			"fixed single-skill REST binding only; cards are advertisement, not authorization or full A2A conformance",
			"compilation is not approval, image provenance, activation attestation or a production deployment transaction",
		}}
	ids := make([]string, 0, len(resolved))
	for id := range resolved {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out.Network, out.Bootstrap = compileCilium(b, resolved, ids)
	out.Services = append(out.Services, service(b.Namespace, b.Model.Workload, 11434))
	out.Model = append(out.Model, modelDeployment(b))
	for _, id := range ids {
		r := resolved[id]
		card := projectAgentCard(r, b.Namespace)
		out.Cards[id] = card
		profile, err := projectFilesystem(pd)
		if err != nil {
			return ApplicationBundle{}, err
		}
		out.Profiles[id] = profile
		gateway, err := compileApplicationGateway(r, resolved, b)
		if err != nil {
			return ApplicationBundle{}, err
		}
		out.Gateways[id] = gateway
		out.Services = append(out.Services, service(b.Namespace, r.Binding.Workload, 8080), service(b.Namespace, "gateway-"+r.Binding.Workload, 3000, 3001))
		workloads, err := agentWorkloads(r, resolved, b, pd, profile, card, gateway)
		if err != nil {
			return ApplicationBundle{}, err
		}
		out.Workloads = append(out.Workloads, workloads...)
	}
	out.PlanDigest, err = Digest(out)
	return out, err
}

func metadata(namespace, name string) Object { return Object{"namespace": namespace, "name": name} }
func service(namespace, name string, ports ...int) Object {
	list := []Object{}
	for _, port := range ports {
		list = append(list, Object{"name": fmt.Sprintf("p%d", port), "port": port, "targetPort": port})
	}
	return Object{"apiVersion": "v1", "kind": "Service", "metadata": metadata(namespace, name),
		"spec": Object{"selector": Object{"app": name}, "ports": list}}
}
func skillPath(a policy.AgentPolicy) string {
	return "/skills/" + a.Capabilities.Skills[0].ID + "/message:send"
}
func serviceEnvironment(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_SERVICE_HOST"
}
