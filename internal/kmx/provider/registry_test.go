package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/provider"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

func TestRegistryRejectsDuplicateRegistration(t *testing.T) {
	t.Parallel()
	registry := provider.NewRegistry()
	factory := stubFactory{descriptor: descriptor("same", 1, kmx.CapabilitySubmit)}
	if err := registry.Register(factory); err != nil {
		t.Fatal(err)
	}
	var duplicate *provider.DuplicateRegistrationError
	if err := registry.Register(factory); !errors.As(err, &duplicate) {
		t.Fatalf("expected duplicate registration error, got %v", err)
	}
}

func TestRegistryResolvesDeterministically(t *testing.T) {
	t.Parallel()
	registry := provider.NewRegistry()
	for _, factory := range []stubFactory{
		{descriptor: descriptor("zeta", 5, kmx.CapabilitySubmit, kmx.CapabilityLogs)},
		{descriptor: descriptor("alpha", 5, kmx.CapabilitySubmit, kmx.CapabilityLogs)},
		{descriptor: descriptor("higher", 10, kmx.CapabilitySubmit)},
	} {
		if err := registry.Register(factory); err != nil {
			t.Fatal(err)
		}
	}
	factory, err := registry.Resolve(provider.ResolutionRequest{
		ContractVersion: kmx.ContractV1Alpha1,
		Capabilities:    []kmx.Capability{kmx.CapabilitySubmit, kmx.CapabilityLogs},
		TargetClass:     "shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := factory.Descriptor().Name; got != "alpha" {
		t.Fatalf("resolved %q, want alpha", got)
	}
}

func TestRegistryReportsUnsupportedCapabilities(t *testing.T) {
	t.Parallel()
	registry := provider.NewRegistry()
	if err := registry.Register(stubFactory{
		descriptor: descriptor("base", 1, kmx.CapabilitySubmit),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := registry.Resolve(provider.ResolutionRequest{
		ContractVersion: kmx.ContractV1Alpha1,
		Capabilities:    []kmx.Capability{kmx.CapabilityArtifacts},
		TargetClass:     "shared",
	})
	if !kmx.IsCode(err, kmx.CodeUnsupported) {
		t.Fatalf("expected typed unsupported error, got %v", err)
	}
}

func TestRegistryMakesConstructionFailuresVisible(t *testing.T) {
	t.Parallel()
	registry := provider.NewRegistry()
	factory := stubFactory{
		descriptor: descriptor("broken", 1, kmx.CapabilitySubmit),
		openErr:    errors.New("configuration refused"),
	}
	if err := registry.Register(factory); err != nil {
		t.Fatal(err)
	}
	_, err := registry.Open(context.Background(), factory, provider.OpenOptions{})
	var construction *provider.ConstructionError
	if !errors.As(err, &construction) || !errors.Is(err, factory.openErr) {
		t.Fatalf("expected visible construction error, got %v", err)
	}
}

type stubFactory struct {
	descriptor provider.Descriptor
	openErr    error
}

func (f stubFactory) Descriptor() provider.Descriptor { return f.descriptor }

func (f stubFactory) Open(context.Context, provider.OpenOptions) (provider.Backend, error) {
	return nil, f.openErr
}

func descriptor(name string, priority int, capabilities ...kmx.Capability) provider.Descriptor {
	return provider.Descriptor{
		Name:             name,
		Priority:         priority,
		ContractVersions: []kmx.ContractVersion{kmx.ContractV1Alpha1},
		Capabilities:     capabilities,
		TargetClasses:    []string{"shared"},
	}
}
