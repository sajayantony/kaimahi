package policy

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDocumentSemantics(t *testing.T) {
	raw, err := os.ReadFile("testdata/deny-all.json")
	if err != nil {
		t.Fatal(err)
	}
	load := func() Document {
		t.Helper()
		var document Document
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		return document
	}
	valid := load()
	valid.Agent = "provider.neutral_agent-1"
	valid.Network.Allow = []Destination{{Scheme: "https", Host: "api.example.com", Port: 443}}
	valid.Invocations.Allow = []Invocation{{Agent: "peer", Skills: []string{"summarize"}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Document){
		"implicit network": func(d *Document) { d.Network.Default = "" },
		"implicit allow":   func(d *Document) { d.Network.Allow = nil },
		"implicit skills":  func(d *Document) { d.Capabilities.Skills = nil },
		"wrong version":    func(d *Document) { d.Capabilities.Version = "0.3.0" },
		"duplicate skill":  func(d *Document) { d.Capabilities.Skills = append(d.Capabilities.Skills, d.Capabilities.Skills[0]) },
		"self grant":       func(d *Document) { d.Invocations.Allow = []Invocation{{Agent: d.Agent, Skills: []string{"summarize"}}} },
		"empty grant":      func(d *Document) { d.Invocations.Allow = []Invocation{{Agent: "peer"}} },
		"IP literal":       func(d *Document) { d.Network.Allow = []Destination{{Scheme: "https", Host: "127.0.0.1", Port: 443}} },
		"port overflow": func(d *Document) {
			d.Network.Allow = []Destination{{Scheme: "https", Host: "api.example.com", Port: 65536}}
		},
		"duplicate destination": func(d *Document) {
			d.Network.Allow = append(valid.Network.Allow[:1:1], valid.Network.Allow[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			document := load()
			mutate(&document)
			if err := document.Validate(); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}
