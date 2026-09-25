package kmx_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

var forbiddenFragments = []string{
	"k8s.io/",
	"kubernetes",
	"azure",
	"os/exec",
	"subprocess",
	"cobra",
	"bubbletea",
	"lipgloss",
	"terminal",
	"internal/kmx",
	"orka",
	"kagent",
}

var forbiddenSerializedFragments = append(
	append([]string(nil), forbiddenFragments...),
	"implementation",
	"resourcekind",
	"command",
	"adapter",
	"backend",
	"provider",
)

func TestPublicPackageHasNoForbiddenImportsOrNames(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(".", entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(data))
		for _, fragment := range forbiddenFragments {
			if strings.Contains(lower, strings.ToLower(fragment)) {
				t.Errorf("%s contains forbidden public fragment %q", path, fragment)
			}
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, `"`)
			if !strings.Contains(importPath, ".") && !strings.Contains(importPath, "/") {
				continue
			}
			for _, fragment := range forbiddenFragments {
				if strings.Contains(strings.ToLower(importPath), strings.ToLower(fragment)) {
					t.Errorf("%s imports forbidden dependency %q", path, importPath)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok || !identifier.IsExported() {
				return true
			}
			lowerName := strings.ToLower(identifier.Name)
			for _, fragment := range forbiddenSerializedFragments {
				if strings.Contains(lowerName, strings.ToLower(fragment)) {
					t.Errorf("%s exports forbidden identifier %q", path, identifier.Name)
				}
			}
			return true
		})
	}
}

func TestPublicSerializedSurfaceIsNeutral(t *testing.T) {
	t.Parallel()
	values := []any{
		kmx.WorkloadSpec{},
		kmx.AgentSpec{},
		kmx.RequirementSet{},
		kmx.TargetSelector{},
		kmx.TargetRef{},
		kmx.ExecutionSnapshot{},
		kmx.ExecutionReceipt{},
		kmx.Event{},
		kmx.LogEntry{},
		kmx.Output{},
		kmx.Artifact{},
		kmx.CancelReceipt{},
		kmx.DeleteReceipt{},
	}
	for _, value := range values {
		assertNeutralType(t, reflect.TypeOf(value), map[reflect.Type]bool{})
	}

	handle, err := kmx.NewExecutionHandle(
		"exec-1",
		kmx.TargetRef{ID: "target-1", Class: "shared"},
		kmx.NewDigest([]byte("spec")),
		kmx.NewDigest([]byte("binding")),
		kmx.ContractV1Alpha1,
		timeForTest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(handle)
	if err != nil {
		t.Fatal(err)
	}
	assertNeutralJSON(t, data)

	receipt, err := kmx.NewExecutionReceipt(
		handle.ID(),
		handle.SpecDigest(),
		handle.BindingDigest(),
		kmx.NewDigest([]byte("result")),
		[]kmx.Evidence{{
			Name:   "target-application",
			Digest: kmx.NewDigest([]byte("evidence")),
			Time:   timeForTest(),
		}},
		timeForTest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	assertNeutralJSON(t, data)
}

func TestPublicDependencyClosureIsNeutral(t *testing.T) {
	t.Parallel()
	command := exec.Command("go", "list", "-deps", "./pkg/kmx")
	command.Dir = filepath.Join("..", "..")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, output)
	}
	lower := strings.ToLower(string(output))
	for _, fragment := range forbiddenSerializedFragments {
		if strings.Contains(lower, strings.ToLower(fragment)) {
			t.Errorf("dependency closure contains forbidden fragment %q", fragment)
		}
	}
}

func assertNeutralType(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if typ.PkgPath() != "" && typ.PkgPath() != "github.com/kaimahi-agents/kaimahi/pkg/kmx" {
		return
	}
	if seen[typ] {
		return
	}
	seen[typ] = true
	if typ.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
		for _, fragment := range forbiddenSerializedFragments {
			if strings.Contains(name, strings.ToLower(fragment)) {
				t.Errorf("%s.%s exposes forbidden fragment %q", typ.Name(), field.Name, fragment)
			}
		}
		assertNeutralType(t, field.Type, seen)
	}
}

func assertNeutralJSON(t *testing.T, data []byte) {
	t.Helper()
	lower := strings.ToLower(string(data))
	for _, fragment := range forbiddenSerializedFragments {
		if strings.Contains(lower, strings.ToLower(fragment)) {
			t.Errorf("serialized handle contains forbidden fragment %q: %s", fragment, data)
		}
	}
}

func timeForTest() time.Time {
	return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
}
