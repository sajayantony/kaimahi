package agentsuite

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPackerProducesValidOCILayout(t *testing.T) {
	source := filepath.Join("testdata", "minimal")
	validator := &recordingValidator{delegate: PathValidator{}}
	packer := NewPacker(validator)
	layout := t.TempDir()
	target, err := NewOCILayoutTarget(layout)
	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := packer.Pack(context.Background(), target, source)
	if err != nil {
		t.Fatal(err)
	}
	if validator.calls != 1 {
		t.Fatalf("validator calls = %d, want 1", validator.calls)
	}
	if !validator.validatedLayout {
		t.Fatal("validator did not receive the staged OCI layout")
	}
	if descriptor.MediaType != ociManifestMediaType || !validDigest(descriptor.Digest) || descriptor.Size <= 0 {
		t.Fatalf("invalid manifest descriptor: %+v", descriptor)
	}
	report, err := ValidatePath(layout)
	if err != nil {
		t.Fatalf("packed layout is invalid: %v", err)
	}
	if report.Name != "minimal" {
		t.Fatalf("suite name = %q, want minimal", report.Name)
	}
}

func TestPackerIsDeterministic(t *testing.T) {
	source := filepath.Join("testdata", "minimal")
	packer := NewPacker(PathValidator{})
	var descriptors []Descriptor
	for range 2 {
		target, err := NewOCILayoutTarget(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := packer.Pack(context.Background(), target, source)
		if err != nil {
			t.Fatal(err)
		}
		descriptors = append(descriptors, descriptor)
	}
	if descriptors[0] != descriptors[1] {
		t.Fatalf("pack descriptors differ: %+v != %+v", descriptors[0], descriptors[1])
	}
}

func TestPackerStopsWhenValidationFails(t *testing.T) {
	target := &recordingTarget{}
	packer := NewPacker(failingValidator{})
	if _, err := packer.Pack(context.Background(), target, filepath.Join("testdata", "minimal")); err == nil ||
		!errors.Is(err, errRejectedSuite) {
		t.Fatalf("Pack() error = %v, want validation failure", err)
	}
	if target.pushes != 0 || target.tags != 0 {
		t.Fatalf("target was mutated after validation failure: %+v", target)
	}
}

func TestOCILayoutTargetRejectsMismatchedContent(t *testing.T) {
	target, err := NewOCILayoutTarget(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	descriptor := Descriptor{
		MediaType: MediaTypeContent,
		Digest:    digestBytes([]byte("expected")),
		Size:      int64(len("expected")),
	}
	if err := target.Push(context.Background(), descriptor, bytes.NewReader([]byte("different"))); err == nil {
		t.Fatal("mismatched content succeeded")
	}
}

func TestPackerRejectsOCILayoutAsSource(t *testing.T) {
	source := filepath.Join("testdata", "minimal")
	packer := NewPacker(PathValidator{})
	layout := t.TempDir()
	target, err := NewOCILayoutTarget(layout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packer.Pack(context.Background(), target, source); err != nil {
		t.Fatal(err)
	}

	next := &recordingTarget{}
	if _, err := packer.Pack(context.Background(), next, layout); err == nil {
		t.Fatal("packing an OCI layout as source succeeded")
	}
	if next.pushes != 0 || next.tags != 0 {
		t.Fatalf("target was mutated for an OCI layout source: %+v", next)
	}
}

type recordingValidator struct {
	delegate        IValidator
	calls           int
	validatedLayout bool
}

func (v *recordingValidator) ValidatePath(path string) (*Report, error) {
	v.calls++
	if _, err := os.Stat(filepath.Join(path, "oci-layout")); err == nil {
		v.validatedLayout = true
	}
	return v.delegate.ValidatePath(path)
}

var errRejectedSuite = errors.New("rejected suite")

type failingValidator struct{}

func (failingValidator) ValidatePath(string) (*Report, error) {
	return nil, errRejectedSuite
}

type recordingTarget struct {
	pushes int
	tags   int
}

func (t *recordingTarget) Push(context.Context, Descriptor, io.Reader) error {
	t.pushes++
	return nil
}

func (t *recordingTarget) Tag(context.Context, Descriptor, string) error {
	t.tags++
	return nil
}
