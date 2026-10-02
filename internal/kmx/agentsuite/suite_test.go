package agentsuite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadResolvesAggregationAndDeterministicDigest(t *testing.T) {
	path := sampleSuitePath(t)
	first, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := second.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest || !strings.HasPrefix(firstDigest, "sha256:") {
		t.Fatalf("digests = %q, %q", firstDigest, secondDigest)
	}
	if first.Aggregation.Member != "invoice-coordinator" ||
		strings.Join(first.Aggregation.Inputs, ",") != "purchasing-agent,receiving-agent" {
		t.Fatalf("aggregation = %+v", first.Aggregation)
	}
	if len(first.Members) != 3 {
		t.Fatalf("members = %d", len(first.Members))
	}
	for _, member := range first.Members {
		if !strings.HasPrefix(member.PortableDigest, "sha256:") || member.Agent() == nil {
			t.Fatalf("member = %+v", member)
		}
	}
}

func TestExportOCIProducesLinkedImageLayout(t *testing.T) {
	suite, err := Load(sampleSuitePath(t))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "layout")
	result, err := ExportOCI(suite, output)
	if err != nil {
		t.Fatal(err)
	}
	if result.SuiteDigest == "" || result.ManifestDigest == "" {
		t.Fatalf("result = %+v", result)
	}
	for _, name := range []string{"oci-layout", "index.json"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	indexBody, err := os.ReadFile(filepath.Join(output, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed index
	if err := json.Unmarshal(indexBody, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Manifests) != 1 || parsed.Manifests[0].Digest != result.ManifestDigest {
		t.Fatalf("index = %+v", parsed)
	}
	manifestPath := filepath.Join(output, "blobs", "sha256", strings.TrimPrefix(result.ManifestDigest, "sha256:"))
	manifestBody, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var artifact manifest
	if err := json.Unmarshal(manifestBody, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.ArtifactType != ArtifactType || artifact.Config.Digest != result.SuiteDigest {
		t.Fatalf("manifest = %+v", artifact)
	}
	if len(artifact.Layers) != 4 {
		t.Fatalf("layers = %d, want suite source plus three agents", len(artifact.Layers))
	}
	loaded, err := LoadInspectable(output)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != suite.Name || loaded.Aggregation.Member != suite.Aggregation.Member {
		t.Fatalf("loaded OCI suite = %+v", loaded)
	}
}

func TestLoadRejectsDeclaredGraphThatDiffersFromCoordinator(t *testing.T) {
	root := copySampleSuite(t)
	path := filepath.Join(root, "suite.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.ReplaceAll(string(body), "      - receiving-agent\n", ""))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(root)
	if err == nil || !strings.Contains(err.Error(), `entrypoint allows "receiving-agent"`) {
		t.Fatalf("error = %v", err)
	}
}

func sampleSuitePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "agent-suite", "invoice-review"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func copySampleSuite(t *testing.T) string {
	t.Helper()
	source := sampleSuitePath(t)
	target := filepath.Join(t.TempDir(), "suite")
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o700)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, body, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	return target
}
