package governance

import (
	"encoding/json"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
)

func compileApplicationGateway(a resolvedAgent, agents map[string]resolvedAgent, b ApplicationBinding) (Object, error) {
	route := func(name, path, backend string) Object {
		return Object{"name": name, "gateways": "http", "matches": []Object{{"path": Object{"exact": path}, "method": "POST"}},
			"policies": Object{"authorization": Object{"rules": []Object{{"require": "true"}}}}, "backends": []Object{{"host": backend}}}
	}
	routes := []Object{route("model-inference", "/v1/chat/completions", b.Model.Workload+"."+b.Namespace+".svc.cluster.local:11434")}
	if a.Peer != "" {
		routes = append(routes, route("peer-skill", skillPath(agents[a.Peer].Policy), agents[a.Peer].Binding.Workload+"."+b.Namespace+".svc.cluster.local:8080"))
	}
	routes = append(routes, Object{"name": "deny-other-http", "gateways": "http",
		"policies": Object{"authorization": Object{"rules": []Object{{"require": "false"}}}}, "backends": []Object{{"host": "127.0.0.1:9"}}})
	out := Object{"config": Object{"adminAddr": "off", "statsAddr": "off", "workerThreads": "2", "enableIpv6": false},
		"gateways": Object{"http": Object{"port": 3001}}, "routes": routes}
	if a.Destination != nil {
		tls, err := ProjectGatewayEgress(policy.Document{APIVersion: policy.Version, Agent: a.Policy.Agent, Capabilities: a.Policy.Capabilities,
			Filesystem: policy.Filesystem{Write: "deny"}, Network: policy.Network{Default: "deny", Allow: []policy.Destination{*a.Destination}},
			Invocations: policy.Invocations{Default: "deny", Allow: []policy.Invocation{}}})
		if err != nil {
			return nil, err
		}
		out["binds"] = tls.Config.Binds
		for name, gateway := range tls.Config.Gateways {
			out["gateways"].(Object)[name] = gateway
		}
		out["tcpRoutes"] = tls.Config.TCPRoutes
	}
	return out, nil
}

func hardenedContainer() Object {
	return Object{"readOnlyRootFilesystem": true, "allowPrivilegeEscalation": false, "capabilities": Object{"drop": []string{"ALL"}}}
}
func deployment(namespace, name string, pod Object) Object {
	return Object{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": metadata(namespace, name),
		"spec": Object{"replicas": 1, "selector": Object{"matchLabels": Object{"app": name}},
			"template": Object{"metadata": Object{"labels": Object{"app": name}}, "spec": pod}}}
}
func modelDeployment(b ApplicationBinding) Object {
	return deployment(b.Namespace, b.Model.Workload, Object{"automountServiceAccountToken": false,
		"containers": []Object{{"name": "model", "image": b.Model.Image,
			"env": []Object{{"name": "OLLAMA_HOST", "value": "0.0.0.0:11434"}, {"name": "OLLAMA_NUM_PARALLEL", "value": "1"},
				{"name": "OLLAMA_CONTEXT_LENGTH", "value": "2048"}, {"name": "CUDA_VISIBLE_DEVICES", "value": "-1"}, {"name": "ROCR_VISIBLE_DEVICES", "value": "-1"}},
			"securityContext": Object{"allowPrivilegeEscalation": false, "capabilities": Object{"drop": []string{"ALL"}}, "seccompProfile": Object{"type": "RuntimeDefault"}},
			"resources":       Object{"requests": Object{"cpu": "100m", "memory": "512Mi"}, "limits": Object{"cpu": "2", "memory": "3Gi"}},
			"readinessProbe":  Object{"tcpSocket": Object{"port": 11434}, "periodSeconds": 2}}}})
}
func agentWorkloads(a resolvedAgent, agents map[string]resolvedAgent, b ApplicationBinding, policyDigest string, profile GatewaySandboxProjection, card AgentCard, gateway Object) ([]Object, error) {
	other := ""
	for id := range agents {
		if id != a.Policy.Agent {
			other = id
		}
	}
	settings := Object{"role": a.Binding.Runtime, "skillPath": skillPath(a.Policy), "publicURL": card.SupportedInterfaces[0].URL,
		"generatedCard": true, "modelProtocol": "openai-compatible", "modelName": b.Model.Model,
		"instructions": a.Instructions,
		"serviceEnvironment": Object{"gatewayIP": serviceEnvironment("gateway-" + a.Binding.Workload),
			"modelIP": serviceEnvironment(b.Model.Workload), "peerIP": serviceEnvironment(agents[other].Binding.Workload),
			"otherGatewayIP": serviceEnvironment("gateway-" + agents[other].Binding.Workload)}}
	if a.Peer != "" {
		settings["peerPath"] = skillPath(agents[a.Peer].Policy)
	}
	if a.Destination != nil {
		settings["registryHost"] = a.Destination.Host
		settings["registryPort"] = a.Destination.Port
	}
	config, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	cardBytes, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	identityBytes, err := json.Marshal(Object{"agent": a.Policy.Agent, "capabilities": a.Policy.Capabilities, "suitePolicyDigest": policyDigest})
	if err != nil {
		return nil, err
	}
	gatewayBytes, err := json.Marshal(gateway)
	if err != nil {
		return nil, err
	}
	name := a.Binding.Workload
	cm := Object{"apiVersion": "v1", "kind": "ConfigMap", "metadata": metadata(b.Namespace, name),
		"immutable": true, "data": Object{"agent.py": cpuAgent, "launcher.py": profile.Launcher, "identity.json": string(identityBytes), "agent-card.json": string(cardBytes)}}
	agent := deployment(b.Namespace, name, Object{"automountServiceAccountToken": false, "enableServiceLinks": true,
		"hostNetwork": false, "hostPID": false, "hostIPC": false,
		"nodeSelector":    Object{"kubernetes.io/arch": "amd64", "agentsuite.dev/profile-" + profile.ProfileDigest[7:39]: "installed"},
		"securityContext": Object{"runAsUser": 65532, "runAsGroup": 65532, "runAsNonRoot": true, "seccompProfile": Object{"type": "Localhost", "localhostProfile": profile.ProfilePath}},
		"volumes":         []Object{{"name": "source", "configMap": Object{"name": name}}},
		"containers": []Object{{"name": "agent", "image": a.Binding.Image, "command": []string{"python", "-B", "/example/launcher.py", "python", "-B", "/example/agent.py"},
			"env": []Object{{"name": "EXAMPLE_CONFIG", "value": string(config)}}, "securityContext": hardenedContainer(),
			"volumeMounts":   []Object{{"name": "source", "mountPath": "/example", "readOnly": true}},
			"resources":      Object{"requests": Object{"cpu": "25m", "memory": "32Mi"}, "limits": Object{"cpu": "500m", "memory": "128Mi"}},
			"readinessProbe": Object{"httpGet": Object{"path": "/healthz", "port": 8080}, "periodSeconds": 2}}}})
	agent["spec"].(Object)["template"].(Object)["metadata"].(Object)["annotations"] = Object{"agentsuite.dev/policy-digest": policyDigest}
	gw := deployment(b.Namespace, "gateway-"+name, Object{"automountServiceAccountToken": false,
		"securityContext": Object{"runAsUser": 65532, "runAsGroup": 65532, "runAsNonRoot": true, "seccompProfile": Object{"type": "RuntimeDefault"},
			"sysctls": []Object{{"name": "net.ipv4.ip_unprivileged_port_start", "value": "0"}}},
		"containers": []Object{{"name": "gateway", "image": b.GatewayImage, "args": []string{"-c", string(gatewayBytes)},
			"securityContext": hardenedContainer(), "resources": Object{"requests": Object{"cpu": "25m", "memory": "64Mi"}, "limits": Object{"cpu": "500m", "memory": "256Mi"}},
			"readinessProbe": Object{"tcpSocket": Object{"port": 3001}, "periodSeconds": 2}}}})
	return []Object{cm, gw, agent}, nil
}
