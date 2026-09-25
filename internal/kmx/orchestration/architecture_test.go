package orchestration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericOrchestrationDoesNotDependOnConcreteImplementations(t *testing.T) {
	t.Parallel()
	command := exec.Command("go", "list", "-deps", "./internal/kmx/orchestration")
	command.Dir = filepath.Join("..", "..", "..")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, output)
	}
	for _, forbidden := range []string{
		"internal/kmx/implementations/",
		"internal/kmx/app",
		"internal/kmx/runtime",
	} {
		if strings.Contains(string(output), forbidden) {
			t.Errorf("generic orchestration depends on %q", forbidden)
		}
	}
}
