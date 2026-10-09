package governance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
)

func TestGatewayEgressProjectionMatchesNativeSample(t *testing.T) {
	root := filepath.Join("..", "..", "..", "agentsuite", "policy")
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "allow-mcr.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := agentsuite.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := ProjectGatewayEgress(document)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(root, "examples", "agentgateway", "allow-mcr.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sample EgressGatewayConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sample); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projection.Config, sample) {
		t.Fatal("checked-in native example differs from the portable policy projection")
	}
	if projection.Scope != "gateway-tls-authorities-only" || len(projection.Unenforced) != 4 {
		t.Fatal("projection must explicitly report partial scope")
	}
	if _, err := Compile(document, Backend); err == nil {
		t.Fatal("gateway support must not silently enable unsupported sandbox allowlists")
	}
	for name, mutate := range map[string]func(*policy.Document){
		"HTTP":             func(d *policy.Document) { d.Network.Allow[0].Scheme = "http" },
		"listener overlap": func(d *policy.Document) { d.Network.Allow[0].Port = 3000 },
		"no allowlist":     func(d *policy.Document) { d.Network.Allow = []policy.Destination{} },
		"A2A grant": func(d *policy.Document) {
			d.Invocations.Allow = []policy.Invocation{{Agent: "peer", Skills: []string{"summarize"}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := document
			copy.Network.Allow = append([]policy.Destination(nil), document.Network.Allow...)
			mutate(&copy)
			if got, err := ProjectGatewayEgress(copy); err == nil || got.PolicyDigest != "" {
				t.Fatal("unsupported policy returned a projection")
			}
		})
	}
	document.Network.Allow = append(document.Network.Allow, policy.Destination{Scheme: "https", Host: "api.example.com", Port: 8443})
	other, err := ProjectGatewayEgress(document)
	if err != nil || other.PolicyDigest == projection.PolicyDigest ||
		other.Config.Gateways["tls-8443"].Port != 8443 ||
		other.Config.TCPRoutes[0].Backends[0].Host != "api.example.com:8443" {
		t.Fatal("projection did not preserve destination identity and exact port")
	}
}
