package provider

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

type DuplicateRegistrationError struct {
	Name string
}

func (e *DuplicateRegistrationError) Error() string {
	return fmt.Sprintf("implementation %q is already registered", e.Name)
}

type ConstructionError struct {
	Name string
	Err  error
}

func (e *ConstructionError) Error() string {
	return fmt.Sprintf("construct implementation %q: %v", e.Name, e.Err)
}

func (e *ConstructionError) Unwrap() error { return e.Err }

type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

func (r *Registry) Register(factory Factory) error {
	if factory == nil || (reflect.ValueOf(factory).Kind() == reflect.Pointer && reflect.ValueOf(factory).IsNil()) {
		return errors.New("register implementation: nil factory")
	}
	descriptor := factory.Descriptor()
	if strings.TrimSpace(descriptor.Name) == "" {
		return errors.New("register implementation: empty name")
	}
	if len(descriptor.ContractVersions) == 0 {
		return fmt.Errorf("register implementation %q: no contract versions", descriptor.Name)
	}
	if len(descriptor.TargetClasses) == 0 {
		return fmt.Errorf("register implementation %q: no target classes", descriptor.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[descriptor.Name]; exists {
		return &DuplicateRegistrationError{Name: descriptor.Name}
	}
	r.factories[descriptor.Name] = factory
	return nil
}

func (r *Registry) Resolve(request ResolutionRequest) (Factory, error) {
	r.mu.RLock()
	factories := make([]Factory, 0, len(r.factories))
	for _, factory := range r.factories {
		factories = append(factories, factory)
	}
	r.mu.RUnlock()

	sort.Slice(factories, func(i, j int) bool {
		left := factories[i].Descriptor()
		right := factories[j].Descriptor()
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		return left.Name < right.Name
	})

	for _, factory := range factories {
		if compatible(factory.Descriptor(), request) {
			return factory, nil
		}
	}
	capability := kmx.Capability("")
	if len(request.Capabilities) > 0 {
		capability = request.Capabilities[0]
	}
	return nil, &kmx.UnsupportedError{Operation: "submit", Capability: capability}
}

func (r *Registry) Open(
	ctx context.Context,
	factory Factory,
	options OpenOptions,
) (Backend, error) {
	backend, err := factory.Open(ctx, options)
	if err != nil {
		return nil, &ConstructionError{Name: factory.Descriptor().Name, Err: err}
	}
	if backend == nil || (reflect.ValueOf(backend).Kind() == reflect.Pointer && reflect.ValueOf(backend).IsNil()) {
		return nil, &ConstructionError{
			Name: factory.Descriptor().Name,
			Err:  errors.New("factory returned nil backend"),
		}
	}
	return backend, nil
}

func compatible(descriptor Descriptor, request ResolutionRequest) bool {
	if !containsVersion(descriptor.ContractVersions, request.ContractVersion) {
		return false
	}
	if !containsString(descriptor.TargetClasses, request.TargetClass) {
		return false
	}
	for _, required := range request.Capabilities {
		if !containsCapability(descriptor.Capabilities, required) {
			return false
		}
	}
	for _, constraint := range request.Constraints {
		value, exists := descriptor.Labels[constraint.Name]
		if !exists {
			return false
		}
		switch constraint.Operator {
		case "equals":
			if len(constraint.Values) != 1 || value != constraint.Values[0] {
				return false
			}
		case "in":
			if !containsString(constraint.Values, value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func containsVersion(values []kmx.ContractVersion, wanted kmx.ContractVersion) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsCapability(values []kmx.Capability, wanted kmx.Capability) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
