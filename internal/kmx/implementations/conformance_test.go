package implementations_test

import (
	"context"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/implementations/batch"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/implementations/interactive"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/provider"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/provider/conformancetest"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/target/memory"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

func TestImplementationsPassBaseConformance(t *testing.T) {
	t.Parallel()
	for _, factory := range []provider.Factory{batch.Factory{}, interactive.Factory{}} {
		factory := factory
		t.Run(factory.Descriptor().Name, func(t *testing.T) {
			t.Parallel()
			targetPort := memory.New(
				kmx.TargetRef{ID: "target-" + factory.Descriptor().Name, Class: "shared"},
				nil,
			)
			facts, err := targetPort.Resolve(
				context.Background(),
				kmx.TargetSelector{Class: "shared"},
			)
			if err != nil {
				t.Fatal(err)
			}
			conformancetest.Run(t, factory, targetPort, facts)
		})
	}
}
