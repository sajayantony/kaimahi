package memory_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/target"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/target/memory"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

func TestTargetLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	port := memory.New(kmx.TargetRef{ID: "target-1", Class: "shared"}, map[string]string{"region": "test"})
	facts, err := port.Resolve(ctx, kmx.TargetSelector{
		Class: "shared",
		Match: map[string]string{"region": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resources := []target.Resource{{LogicalName: "unit", Data: []byte("data")}}
	if _, err := port.Validate(ctx, facts, target.ValidationRequest{
		ExecutionID: "exec-1",
		Resources:   resources,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := port.Apply(ctx, facts, target.ApplyRequest{
		ExecutionID: "exec-1",
		Resources:   resources,
	}); err != nil {
		t.Fatal(err)
	}
	observation, err := port.Read(ctx, facts, target.ReadRequest{ExecutionID: "exec-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Found {
		t.Fatal("applied execution was not observed")
	}
	observations, err := port.Watch(ctx, facts, target.WatchRequest{ExecutionID: "exec-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observations.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := observations.Next(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("expected end of observation stream, got %v", err)
	}
	receipt, err := port.Delete(ctx, facts, target.DeleteRequest{ExecutionID: "exec-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Deleted {
		t.Fatal("delete did not remove execution")
	}
}
