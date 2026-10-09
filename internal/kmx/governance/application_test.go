package governance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func applicationInput(t *testing.T, mode string) (*agentsuite.DeploymentSuite, policy.SuitePolicy, ApplicationBinding) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "agentsuite", "policy", "examples", "suite-application")
	var external []byte
	var err error
	if mode == "external" {
		external, err = os.ReadFile(filepath.Join(root, "suite-policy.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	suite, p, err := agentsuite.ResolvePolicyDeploymentSuite(filepath.Join(root, mode), agentsuite.Platform{OS: "linux", Architecture: "amd64"}, external)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "binding-"+mode+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var binding ApplicationBinding
	if err := agentsuite.DecodeStrictJSON(data, &binding); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("application-binding.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
	return suite, p, binding
}

func TestApplicationBundledAndExternalEquivalent(t *testing.T) {
	var baseline ApplicationBundle
	for _, mode := range []string{"bundled", "external"} {
		suite, p, binding := applicationInput(t, mode)
		out, err := CompileApplication(suite, p, binding)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := CompileApplication(suite, p, binding)
		if err != nil {
			t.Fatal(err)
		}
		if out.PlanDigest != repeated.PlanDigest {
			t.Fatal("non-deterministic conversion")
		}
		if len(out.Network) != 7 || len(out.Cards) != 2 || len(out.Workloads) != 6 || len(out.Profiles) != 2 {
			t.Fatal("incomplete application")
		}
		for _, profile := range out.Profiles {
			if profile.PolicyDigest != out.PolicyDigest {
				t.Fatal("filesystem projection must bind the complete suite policy")
			}
		}
		for _, object := range out.Workloads {
			if object["kind"] == "ConfigMap" {
				data := object["data"].(Object)
				if _, ok := data["policy.json"]; ok {
					t.Fatal("agent identity must not masquerade as partial policy")
				}
				var identity struct {
					SuitePolicyDigest string `json:"suitePolicyDigest"`
				}
				if err := json.Unmarshal([]byte(data["identity.json"].(string)), &identity); err != nil || identity.SuitePolicyDigest != out.PolicyDigest {
					t.Fatal("agent identity must reference the full suite policy")
				}
			}
		}
		for _, object := range out.Network {
			if object["apiVersion"] != "cilium.io/v2" || object["kind"] != "CiliumNetworkPolicy" {
				t.Fatal("adapter did not emit native Cilium policy")
			}
		}
		reader := out.Network[5]
		raw, err := json.Marshal(out.Network)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"matchName":"mcr.microsoft.com"`) || reader == nil {
			t.Fatal("missing exact FQDN policy")
		}
		out.SuiteDigest, out.BindingDigest, out.PlanDigest = "", "", ""
		if mode == "bundled" {
			baseline = out
		} else {
			a, err := Digest(baseline)
			if err != nil {
				t.Fatal(err)
			}
			b, err := Digest(out)
			if err != nil {
				t.Fatal(err)
			}
			if a != b {
				t.Fatal("policy delivery mode changed generated enforcement")
			}
		}
	}
}

func TestApplicationBindingAndPolicyFailures(t *testing.T) {
	for name, mutate := range map[string]func(*policy.SuitePolicy, *ApplicationBinding){
		"suite digest": func(p *policy.SuitePolicy, b *ApplicationBinding) {
			b.SuiteDigest = "sha256:" + strings.Repeat("0", 64)
		},
		"policy digest": func(p *policy.SuitePolicy, b *ApplicationBinding) {
			b.PolicyDigest = "sha256:" + strings.Repeat("0", 64)
		},
		"adapter":        func(p *policy.SuitePolicy, b *ApplicationBinding) { b.Adapter = "unknown" },
		"missing member": func(p *policy.SuitePolicy, b *ApplicationBinding) { delete(b.Agents, "registry-reader") },
		"label collision": func(p *policy.SuitePolicy, b *ApplicationBinding) {
			a := b.Agents["coordinator"]
			a.Workload = "cpu-model"
			b.Agents["coordinator"] = a
		},
		"unpinned image": func(p *policy.SuitePolicy, b *ApplicationBinding) { b.GatewayImage = "gateway:latest" },
		"reserved model": func(p *policy.SuitePolicy, b *ApplicationBinding) { b.Model.Workload = "model-bootstrap" },
		"reserved workload": func(p *policy.SuitePolicy, b *ApplicationBinding) {
			a := b.Agents["coordinator"]
			a.Workload = "default-deny"
			b.Agents["coordinator"] = a
		},
		"invalid service name": func(p *policy.SuitePolicy, b *ApplicationBinding) { b.Model.Workload = "1model" },
		"undeclared peer": func(p *policy.SuitePolicy, b *ApplicationBinding) {
			p.Agents[0].Invocations = []policy.Invocation{}
			b.PolicyDigest, _ = Digest(p)
		},
	} {
		t.Run(name, func(t *testing.T) {
			suite, p, b := applicationInput(t, "external")
			mutate(&p, &b)
			if out, err := CompileApplication(suite, p, b); err == nil || out.PlanDigest != "" {
				t.Fatal("unsupported application returned a plan")
			}
		})
	}
}

func TestApplicationDeploymentMetadataIsSeparate(t *testing.T) {
	suite, p, b := applicationInput(t, "bundled")
	before, err := CompileApplication(suite, p, b)
	if err != nil {
		t.Fatal(err)
	}
	b.Namespace = "other-namespace"
	a := b.Agents["registry-reader"]
	a.Workload = "bound-reader"
	b.Agents["registry-reader"] = a
	after, err := CompileApplication(suite, p, b)
	if err != nil {
		t.Fatal(err)
	}
	if before.PolicyDigest != after.PolicyDigest || before.PlanDigest == after.PlanDigest {
		t.Fatal("portable intent and deployment identity were conflated")
	}
	if !strings.Contains(after.Cards["registry-reader"].SupportedInterfaces[0].URL, "bound-reader.other-namespace") {
		t.Fatal("card did not use deployment metadata")
	}
	raw, err := json.Marshal(after.Gateways["coordinator"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "bound-reader.other-namespace") {
		t.Fatal("gateway backend did not use the same binding")
	}
	raw, err = json.Marshal(after.Network)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"k8s:app":"bound-reader"`) {
		t.Fatal("Cilium selector did not use the same binding")
	}
}
