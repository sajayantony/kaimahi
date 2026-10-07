package agentsuite

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// IValidator validates an AgentSuite source before it is packaged.
type IValidator interface {
	ValidatePath(string) (*Report, error)
}

// IPacker packages a validated AgentSuite into an OCI target.
type IPacker interface {
	Pack(context.Context, Target, string) (Descriptor, error)
}

// Target is the minimal content-addressed target needed to pack an AgentSuite.
// It mirrors the Push and Tag split used by ORAS without depending on ORAS.
type Target interface {
	Push(context.Context, Descriptor, io.Reader) error
	Tag(context.Context, Descriptor, string) error
}

// PathValidator adapts the package validator to IValidator.
type PathValidator struct{}

func (PathValidator) ValidatePath(path string) (*Report, error) {
	return ValidatePath(path)
}

// Packer packages AgentSuite content after validation.
type Packer struct {
	validator IValidator
}

func NewPacker(validator IValidator) *Packer {
	if validator == nil {
		validator = PathValidator{}
	}
	return &Packer{validator: validator}
}

func (p *Packer) Pack(ctx context.Context, target Target, source string) (Descriptor, error) {
	if target == nil {
		return Descriptor{}, errors.New("AgentSuite pack target is required")
	}
	if p == nil || p.validator == nil {
		return Descriptor{}, errors.New("AgentSuite pack validator is required")
	}
	if err := ctx.Err(); err != nil {
		return Descriptor{}, err
	}
	layer, layerDescriptor, err := packContentLayer(ctx, source)
	if err != nil {
		return Descriptor{}, err
	}
	defer func() {
		layer.Close()
		os.Remove(layer.Name())
	}()

	manifestBytes, err := json.Marshal(ociManifest{
		SchemaVersion: 2,
		MediaType:     ociManifestMediaType,
		ArtifactType:  MediaTypeArtifact,
		Config:        toOCIDescriptor(descriptorFromBytes(MediaTypeEmptyConfig, emptyConfigBytes)),
		Layers:        []ociDescriptor{toOCIDescriptor(layerDescriptor)},
	})
	if err != nil {
		return Descriptor{}, fmt.Errorf("encode AgentSuite OCI manifest: %w", err)
	}
	configDescriptor := descriptorFromBytes(MediaTypeEmptyConfig, emptyConfigBytes)
	manifestDescriptor := descriptorFromBytes(ociManifestMediaType, manifestBytes)

	stageRoot, err := os.MkdirTemp("", "agentsuite-layout-*")
	if err != nil {
		return Descriptor{}, err
	}
	defer os.RemoveAll(stageRoot)
	stage, err := NewOCILayoutTarget(stageRoot)
	if err != nil {
		return Descriptor{}, err
	}
	if err := pushPackedArtifact(ctx, stage, layer, configDescriptor, layerDescriptor, manifestDescriptor, manifestBytes, ""); err != nil {
		return Descriptor{}, fmt.Errorf("stage AgentSuite artifact: %w", err)
	}
	report, err := p.validator.ValidatePath(stageRoot)
	if err != nil {
		return Descriptor{}, fmt.Errorf("validate packed AgentSuite: %w", err)
	}
	if report == nil {
		return Descriptor{}, errors.New("validate packed AgentSuite: validator returned no report")
	}
	if err := pushPackedArtifact(ctx, target, layer, configDescriptor, layerDescriptor, manifestDescriptor, manifestBytes, report.Name); err != nil {
		return Descriptor{}, err
	}
	return manifestDescriptor, nil
}

func pushPackedArtifact(
	ctx context.Context,
	target Target,
	layer *os.File,
	configDescriptor Descriptor,
	layerDescriptor Descriptor,
	manifestDescriptor Descriptor,
	manifestBytes []byte,
	reference string,
) error {
	if err := target.Push(ctx, configDescriptor, bytes.NewReader(emptyConfigBytes)); err != nil {
		return fmt.Errorf("push AgentSuite config: %w", err)
	}
	if _, err := layer.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := target.Push(ctx, layerDescriptor, layer); err != nil {
		return fmt.Errorf("push AgentSuite content: %w", err)
	}
	if err := target.Push(ctx, manifestDescriptor, bytes.NewReader(manifestBytes)); err != nil {
		return fmt.Errorf("push AgentSuite manifest: %w", err)
	}
	if err := target.Tag(ctx, manifestDescriptor, reference); err != nil {
		return fmt.Errorf("tag AgentSuite manifest: %w", err)
	}
	return nil
}

func packContentLayer(ctx context.Context, source string) (*os.File, Descriptor, error) {
	content, err := loadDirectory(source)
	if err != nil {
		return nil, Descriptor{}, fmt.Errorf("load AgentSuite content: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, Descriptor{}, fmt.Errorf("resolve AgentSuite source: %w", err)
	}
	root, err := os.OpenRoot(canonicalRoot)
	if err != nil {
		return nil, Descriptor{}, fmt.Errorf("open AgentSuite source: %w", err)
	}
	defer root.Close()

	file, err := os.CreateTemp("", "agentsuite-content-*.tar.gz")
	if err != nil {
		return nil, Descriptor{}, err
	}
	cleanup := func(packErr error) (*os.File, Descriptor, error) {
		file.Close()
		os.Remove(file.Name())
		return nil, Descriptor{}, packErr
	}

	gzipWriter := gzip.NewWriter(file)
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)

	names := make([]string, 0, len(content.entries))
	for name := range content.entries {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return cleanup(err)
		}
		entry := content.entries[name]
		header := &tar.Header{
			Name:       entry.Path,
			Mode:       int64(entry.Mode),
			Uid:        entry.UID,
			Gid:        entry.GID,
			Size:       entry.Size,
			ModTime:    time.Unix(0, 0).UTC(),
			AccessTime: time.Time{},
			ChangeTime: time.Time{},
			Format:     tar.FormatPAX,
		}
		switch entry.Type {
		case "directory":
			header.Name += "/"
			header.Typeflag = tar.TypeDir
			header.Size = 0
		case "file":
			header.Typeflag = tar.TypeReg
		case "symlink":
			target, err := root.Readlink(entry.Path)
			if err != nil {
				return cleanup(fmt.Errorf("read %s link: %w", entry.Path, err))
			}
			if filepath.ToSlash(target) != entry.LinkTarget {
				return cleanup(fmt.Errorf("%s changed after validation", entry.Path))
			}
			header.Typeflag = tar.TypeSymlink
			header.Linkname = entry.LinkTarget
			header.Size = 0
		default:
			return cleanup(fmt.Errorf("%s has unsupported pack entry type %s", entry.Path, entry.Type))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return cleanup(fmt.Errorf("write %s header: %w", entry.Path, err))
		}
		if entry.Type != "file" {
			continue
		}
		input, err := root.Open(entry.Path)
		if err != nil {
			return cleanup(fmt.Errorf("open %s: %w", entry.Path, err))
		}
		hash := sha256.New()
		written, copyErr := io.CopyN(tarWriter, io.TeeReader(input, hash), entry.Size)
		var extra [1]byte
		_, extraErr := input.Read(extra[:])
		closeErr := input.Close()
		if copyErr != nil || written != entry.Size {
			return cleanup(fmt.Errorf("read %s: %w", entry.Path, copyErr))
		}
		if !errors.Is(extraErr, io.EOF) {
			if extraErr == nil {
				extraErr = errors.New("file grew while packing")
			}
			return cleanup(fmt.Errorf("read %s: %w", entry.Path, extraErr))
		}
		if closeErr != nil {
			return cleanup(fmt.Errorf("close %s: %w", entry.Path, closeErr))
		}
		if digest := "sha256:" + hex.EncodeToString(hash.Sum(nil)); digest != entry.Digest {
			return cleanup(fmt.Errorf("%s changed after validation", entry.Path))
		}
	}
	if err := tarWriter.Close(); err != nil {
		return cleanup(fmt.Errorf("finish AgentSuite tar: %w", err))
	}
	if err := gzipWriter.Close(); err != nil {
		return cleanup(fmt.Errorf("finish AgentSuite gzip: %w", err))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return cleanup(err)
	}
	digest, size, err := digestReader(file)
	if err != nil {
		return cleanup(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return cleanup(err)
	}
	return file, Descriptor{MediaType: MediaTypeContent, Digest: digest, Size: size}, nil
}

func descriptorFromBytes(mediaType string, data []byte) Descriptor {
	return Descriptor{MediaType: mediaType, Digest: digestBytes(data), Size: int64(len(data))}
}

func toOCIDescriptor(descriptor Descriptor) ociDescriptor {
	return ociDescriptor{
		MediaType: descriptor.MediaType,
		Digest:    descriptor.Digest,
		Size:      descriptor.Size,
	}
}
