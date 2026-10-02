package agentsuite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	manifestMediaType = "application/vnd.oci.image.manifest.v1+json"
	indexMediaType    = "application/vnd.oci.image.index.v1+json"
)

type descriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int               `json:"size"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

type manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	ArtifactType  string            `json:"artifactType"`
	Config        descriptor        `json:"config"`
	Layers        []descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations"`
}

type index struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Manifests     []descriptor `json:"manifests"`
}

type PackageResult struct {
	SuiteDigest    string
	ManifestDigest string
	Layout         string
}

func LoadInspectable(path string) (*Resolved, error) {
	if _, err := os.Stat(filepath.Join(path, "oci-layout")); err == nil {
		return LoadOCI(path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect suite source: %w", err)
	}
	return Load(path)
}

func LoadOCI(root string) (*Resolved, error) {
	indexBody, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("read OCI index: %w", err)
	}
	var parsed index
	if err := decodeJSON(indexBody, &parsed); err != nil {
		return nil, fmt.Errorf("invalid OCI index: %w", err)
	}
	if parsed.SchemaVersion != 2 || parsed.MediaType != indexMediaType || len(parsed.Manifests) != 1 {
		return nil, fmt.Errorf("OCI index must contain exactly one image manifest")
	}
	manifestBody, err := readDescriptorBlob(root, parsed.Manifests[0])
	if err != nil {
		return nil, err
	}
	var artifact manifest
	if err := decodeJSON(manifestBody, &artifact); err != nil {
		return nil, fmt.Errorf("invalid OCI manifest: %w", err)
	}
	if artifact.SchemaVersion != 2 || artifact.MediaType != manifestMediaType || artifact.ArtifactType != ArtifactType {
		return nil, fmt.Errorf("OCI manifest is not a Kaimahi Agent Suite artifact")
	}
	if artifact.Config.MediaType != ConfigMediaType {
		return nil, fmt.Errorf("OCI suite config has media type %q", artifact.Config.MediaType)
	}
	configBody, err := readDescriptorBlob(root, artifact.Config)
	if err != nil {
		return nil, err
	}
	var suite Resolved
	if err := decodeJSON(configBody, &suite); err != nil {
		return nil, fmt.Errorf("invalid resolved suite config: %w", err)
	}
	if suite.APIVersion != APIVersion || suite.Kind != "ResolvedAgentSuite" || suite.Name == "" || len(suite.Members) == 0 {
		return nil, fmt.Errorf("resolved suite config is incomplete")
	}
	if expected := artifact.Annotations["kmx.kaimahi.dev/suite-digest"]; expected != artifact.Config.Digest {
		return nil, fmt.Errorf("OCI manifest suite digest annotation does not match config")
	}
	return &suite, nil
}

func ExportOCI(suite *Resolved, output string) (PackageResult, error) {
	if suite == nil {
		return PackageResult{}, fmt.Errorf("resolved suite is required")
	}
	if strings.TrimSpace(output) == "" {
		return PackageResult{}, fmt.Errorf("OCI output directory is required")
	}
	if entries, err := os.ReadDir(output); err == nil && len(entries) != 0 {
		return PackageResult{}, fmt.Errorf("OCI output directory must be empty")
	} else if err != nil && !os.IsNotExist(err) {
		return PackageResult{}, fmt.Errorf("inspect OCI output directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(output, "blobs", "sha256"), 0o755); err != nil {
		return PackageResult{}, fmt.Errorf("create OCI layout: %w", err)
	}

	config, err := suite.Config()
	if err != nil {
		return PackageResult{}, err
	}
	configDescriptor, err := writeBlob(output, ConfigMediaType, config)
	if err != nil {
		return PackageResult{}, err
	}
	sourceDescriptor, err := writeBlob(output, SuiteMediaType, suite.Source())
	if err != nil {
		return PackageResult{}, err
	}
	sourceDescriptor.Annotations = map[string]string{"org.opencontainers.image.title": "suite.yaml"}
	layers := []descriptor{sourceDescriptor}
	for _, member := range suite.Members {
		item, err := writeBlob(output, AgentMediaType, member.SourceBytes())
		if err != nil {
			return PackageResult{}, err
		}
		item.Annotations = map[string]string{
			"org.opencontainers.image.title":  filepath.ToSlash(filepath.Join(member.Source, "agent.yaml")),
			"kmx.kaimahi.dev/suite-member":    member.Name,
			"kmx.kaimahi.dev/portable-digest": member.PortableDigest,
		}
		layers = append(layers, item)
	}
	suiteDigest := configDescriptor.Digest
	manifestBody, err := json.Marshal(manifest{
		SchemaVersion: 2,
		MediaType:     manifestMediaType,
		ArtifactType:  ArtifactType,
		Config:        configDescriptor,
		Layers:        layers,
		Annotations: map[string]string{
			"org.opencontainers.image.title":     suite.Name,
			"kmx.kaimahi.dev/suite-digest":       suiteDigest,
			"kmx.kaimahi.dev/runtime":            suite.Runtime,
			"kmx.kaimahi.dev/aggregation-member": suite.Aggregation.Member,
		},
	})
	if err != nil {
		return PackageResult{}, fmt.Errorf("encode OCI manifest: %w", err)
	}
	manifestBody = append(manifestBody, '\n')
	manifestDescriptor, err := writeBlob(output, manifestMediaType, manifestBody)
	if err != nil {
		return PackageResult{}, err
	}
	manifestDescriptor.ArtifactType = ArtifactType
	manifestDescriptor.Annotations = map[string]string{
		"org.opencontainers.image.ref.name": suite.Name,
		"kmx.kaimahi.dev/suite-digest":      suiteDigest,
	}
	indexBody, err := json.Marshal(index{
		SchemaVersion: 2,
		MediaType:     indexMediaType,
		Manifests:     []descriptor{manifestDescriptor},
	})
	if err != nil {
		return PackageResult{}, fmt.Errorf("encode OCI index: %w", err)
	}
	indexBody = append(indexBody, '\n')
	if err := os.WriteFile(filepath.Join(output, "index.json"), indexBody, 0o644); err != nil {
		return PackageResult{}, fmt.Errorf("write OCI index: %w", err)
	}
	layoutBody := []byte("{\"imageLayoutVersion\":\"1.0.0\"}\n")
	if err := os.WriteFile(filepath.Join(output, "oci-layout"), layoutBody, 0o644); err != nil {
		return PackageResult{}, fmt.Errorf("write OCI layout marker: %w", err)
	}
	return PackageResult{SuiteDigest: suiteDigest, ManifestDigest: manifestDescriptor.Digest, Layout: output}, nil
}

func readDescriptorBlob(root string, item descriptor) ([]byte, error) {
	if !strings.HasPrefix(item.Digest, "sha256:") {
		return nil, fmt.Errorf("OCI descriptor digest %q is not sha256", item.Digest)
	}
	body, err := os.ReadFile(filepath.Join(root, "blobs", "sha256", strings.TrimPrefix(item.Digest, "sha256:")))
	if err != nil {
		return nil, fmt.Errorf("read OCI blob %s: %w", item.Digest, err)
	}
	if len(body) != item.Size || digest(body) != item.Digest {
		return nil, fmt.Errorf("OCI blob %s failed size or digest verification", item.Digest)
	}
	return body, nil
}

func decodeJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	return nil
}

func writeBlob(root, mediaType string, body []byte) (descriptor, error) {
	d := digest(body)
	path := filepath.Join(root, "blobs", "sha256", strings.TrimPrefix(d, "sha256:"))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return descriptor{}, fmt.Errorf("write OCI blob: %w", err)
	}
	return descriptor{MediaType: mediaType, Digest: d, Size: len(body)}, nil
}
