package agentsuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OCILayoutTarget stores packed blobs and the tagged root in an OCI image layout.
type OCILayoutTarget struct {
	root string
}

func NewOCILayoutTarget(root string) (*OCILayoutTarget, error) {
	if root == "" {
		return nil, errors.New("OCI layout root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "blobs", "sha256"), 0o755); err != nil {
		return nil, err
	}
	return &OCILayoutTarget{root: root}, nil
}

func (t *OCILayoutTarget) Push(ctx context.Context, descriptor Descriptor, content io.Reader) error {
	if !validDescriptor(descriptor) {
		return errors.New("invalid content descriptor")
	}
	if !strings.HasPrefix(descriptor.Digest, "sha256:") {
		return errors.New("OCI layout target supports only sha256 descriptors")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(t.root, "blobs", "sha256", strings.TrimPrefix(descriptor.Digest, "sha256:"))
	if _, err := os.Stat(path); err == nil {
		if err := verifyLayoutBlob(path, descriptor); err == nil {
			return nil
		} else {
			return fmt.Errorf("existing OCI blob does not match descriptor: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".push-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(content, descriptor.Size+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != descriptor.Size {
		return fmt.Errorf("content size %d does not match descriptor size %d", written, descriptor.Size)
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if digest != descriptor.Digest {
		return errors.New("content digest does not match descriptor")
	}
	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	return nil
}

func (t *OCILayoutTarget) Tag(ctx context.Context, descriptor Descriptor, reference string) error {
	if descriptor.MediaType != ociManifestMediaType || !validDescriptor(descriptor) {
		return errors.New("AgentSuite tag requires a valid OCI manifest descriptor")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	blobPath := filepath.Join(t.root, "blobs", "sha256", strings.TrimPrefix(descriptor.Digest, "sha256:"))
	if err := verifyLayoutBlob(blobPath, descriptor); err != nil {
		return fmt.Errorf("tagged manifest blob does not match descriptor: %w", err)
	}
	layoutBytes, err := json.Marshal(ociLayout{ImageLayoutVersion: LayoutVersion})
	if err != nil {
		return err
	}
	rootDescriptor := toOCIDescriptor(descriptor)
	rootDescriptor.ArtifactType = MediaTypeArtifact
	if reference != "" {
		rootDescriptor.Annotations = map[string]string{"org.opencontainers.image.ref.name": reference}
	}
	indexBytes, err := json.Marshal(ociIndex{
		SchemaVersion: 2,
		MediaType:     ociIndexMediaType,
		Manifests:     []ociDescriptor{rootDescriptor},
	})
	if err != nil {
		return err
	}
	if err := writeLayoutFile(filepath.Join(t.root, "oci-layout"), layoutBytes); err != nil {
		return err
	}
	return writeLayoutFile(filepath.Join(t.root, "index.json"), indexBytes)
}

func writeLayoutFile(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".layout-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func verifyLayoutBlob(path string, descriptor Descriptor) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != descriptor.Size {
		return errors.New("blob size or type does not match descriptor")
	}
	digest, size, err := digestReader(file)
	if err != nil {
		return err
	}
	if size != descriptor.Size || digest != descriptor.Digest {
		return errors.New("blob content does not match descriptor")
	}
	return nil
}
