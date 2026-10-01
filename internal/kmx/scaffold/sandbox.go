package scaffold

import (
	"encoding/json"
	"fmt"
)

func addSandboxAnnotations(annotations map[string]any, backend string, requirements any) error {
	if backend == "" {
		return nil
	}
	if err := ValidateSingleLineText(backend); err != nil {
		return fmt.Errorf("sandbox backend %w", err)
	}
	encoded, err := json.Marshal(requirements)
	if err != nil {
		return fmt.Errorf("encode sandbox requirements: %w", err)
	}
	annotations["sandbox.kaimahi.dev/backend"] = backend
	annotations["sandbox.kaimahi.dev/requirements"] = string(encoded)
	return nil
}
