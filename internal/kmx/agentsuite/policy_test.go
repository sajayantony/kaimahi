package agentsuite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestPolicyStrictDecodingAndSchema(t *testing.T) {
	root := filepath.Join("..", "..", "..", "agentsuite", "policy")
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "deny-all.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Capabilities.Skills) != 1 || len(document.Invocations.Allow) != 0 {
		t.Fatal("advertisement became an invocation grant")
	}
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(filepath.Join(root, "policy.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	validate := func(raw []byte) error {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		return schema.Validate(value)
	}
	if err := validate(raw); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"allow-mcr.json", "a2a-peer-request.json"} {
		sample, err := os.ReadFile(filepath.Join(root, "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodePolicy(sample); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := validate(sample); err != nil {
			t.Fatalf("%s schema: %v", name, err)
		}
	}
	for _, mutation := range []struct{ old, new string }{
		{`"write": "deny"`, `"write": "allow"`},
		{`"write": "deny"`, `"write": null`},
		{`"write": "deny"`, `"Write": "deny"`},
		{`"write": "deny"`, `"write": "deny", "paths": ["/tmp"]`},
		{`"allow": []`, `"allow": null`},
		{`"allow": []`, `"allow": [{"scheme":"https","host":"example.com/path","port":443}]`},
		{`"allow": []`, `"allow": [{"scheme":"https","host":"*.example.com","port":443}]`},
		{`"allow": []`, `"allow": [{"scheme":"https","host":"example.com","port":0}]`},
	} {
		bad := []byte(strings.Replace(string(raw), mutation.old, mutation.new, 1))
		if _, err := DecodePolicy(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
		if err := validate(bad); err == nil {
			t.Fatalf("schema accepted %s", bad)
		}
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"write": "deny"`, `"write":"deny","write":"deny"`, 1),
		string(raw) + `{}`,
	} {
		if _, err := DecodePolicy([]byte(bad)); err == nil {
			t.Fatal("accepted ambiguous JSON")
		}
	}
}
