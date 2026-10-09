package agentsuite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func suitePolicyExample() string {
	return filepath.Join("..", "..", "..", "agentsuite", "policy", "examples", "suite-application")
}
func TestSuitePolicyBundledAndExternal(t *testing.T) {
	root := suitePolicyExample()
	external, err := os.ReadFile(filepath.Join(root, "suite-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"bundled", "external"} {
		var supplied []byte
		if mode == "external" {
			supplied = external
		}
		suite, p, err := ResolvePolicyDeploymentSuite(filepath.Join(root, mode), Platform{OS: "linux", Architecture: "amd64"}, supplied)
		if err != nil {
			t.Fatal(mode, err)
		}
		if suite.Name() != p.Suite || len(suite.Members()) != 2 {
			t.Fatal("incomplete suite snapshot")
		}
	}
	if _, _, err := ResolvePolicyDeploymentSuite(filepath.Join(root, "external"), Platform{OS: "linux", Architecture: "amd64"}, nil); err == nil {
		t.Fatal("missing policy was accepted")
	}
	changed := []byte(strings.ReplaceAll(string(external), "mcr.microsoft.com", "example.com"))
	if _, _, err := ResolvePolicyDeploymentSuite(filepath.Join(root, "bundled"), Platform{OS: "linux", Architecture: "amd64"}, changed); err == nil {
		t.Fatal("external policy overrode bundled policy")
	}
	if _, err := ResolveDeploymentSuite(filepath.Join(root, "bundled"), Platform{OS: "linux", Architecture: "amd64"}); err == nil {
		t.Fatal("legacy deployment silently ignored policy")
	}
	if _, err := ResolveSandboxPlan(filepath.Join(root, "bundled"), BuildSelection{Agent: "coordinator", Platform: "linux/amd64"}); err == nil {
		t.Fatal("legacy builder silently ignored policy")
	}
}

func TestSuitePolicyReferenceIntegrity(t *testing.T) {
	for _, mutation := range []string{"changed-policy", "null-reference"} {
		t.Run(mutation, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "suite")
			if err := os.CopyFS(dir, os.DirFS(filepath.Join(suitePolicyExample(), "bundled"))); err != nil {
				t.Fatal(err)
			}
			if mutation == "changed-policy" {
				file := filepath.Join(dir, "policy", "suite-policy.json")
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(strings.ReplaceAll(string(data), "mcr.microsoft.com", "example.com")), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				file := filepath.Join(dir, "agentsuite.json")
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var v map[string]any
				if err := json.Unmarshal(data, &v); err != nil {
					t.Fatal(err)
				}
				v["policy"] = nil
				data, err = json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := ResolvePolicyDeploymentSuite(dir, Platform{OS: "linux", Architecture: "amd64"}, nil); err == nil {
				t.Fatal("invalid policy reference accepted")
			}
		})
	}
}

func TestSuitePolicySchemaAndNoVendorFields(t *testing.T) {
	root := filepath.Join("..", "..", "..", "agentsuite", "policy")
	compiler := jsonschema.NewCompiler()
	data, err := os.ReadFile(filepath.Join(root, "policy.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("https://agentsuite.dev/experimental/policy.schema.json", schema); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(filepath.Join(root, "suite.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(suitePolicyExample(), "suite-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"valid", "vendor", "null-destination"} {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if mutation == "vendor" {
			value["endpointSelector"] = map[string]any{"matchLabels": map[string]string{"app": "agent"}}
		}
		if mutation == "null-destination" {
			value["resources"].([]any)[0].(map[string]any)["destination"] = nil
		}
		bytes, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, decodeErr := DecodeSuitePolicy(bytes)
		schemaErr := compiled.Validate(value)
		invalid := mutation != "valid"
		if (decodeErr != nil) != invalid || (schemaErr != nil) != invalid {
			t.Fatalf("mutation=%s decode=%v schema=%v", mutation, decodeErr, schemaErr)
		}
	}
}

func TestSuitePolicySemanticFailures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(suitePolicyExample(), "suite-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*policy.SuitePolicy){
		"default allow":        func(p *policy.SuitePolicy) { p.Defaults.Network = "allow" },
		"duplicate resource":   func(p *policy.SuitePolicy) { p.Resources = append(p.Resources, p.Resources[0]) },
		"undeclared resource":  func(p *policy.SuitePolicy) { p.Agents[0].Resources = []string{"missing"} },
		"duplicate agent":      func(p *policy.SuitePolicy) { p.Agents = append(p.Agents, p.Agents[0]) },
		"undeclared peer":      func(p *policy.SuitePolicy) { p.Agents[0].Invocations[0].Agent = "missing" },
		"unadvertised skill":   func(p *policy.SuitePolicy) { p.Agents[0].Invocations[0].Skills = []string{"missing"} },
		"implicit invocations": func(p *policy.SuitePolicy) { p.Agents[1].Invocations = nil },
	} {
		t.Run(name, func(t *testing.T) {
			p, err := DecodeSuitePolicy(raw)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("invalid portable policy accepted")
			}
		})
	}
}
