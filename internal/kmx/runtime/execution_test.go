package runtime

import (
	"context"
	"fmt"
	"testing"
)

type exampleExecutionObserver struct{ id ID }

func (a exampleExecutionObserver) ID() ID                                      { return a.id }
func (exampleExecutionObserver) Probe(context.Context, Target) (Probe, error)  { return Probe{}, nil }
func (exampleExecutionObserver) Open(context.Context, Target) (Session, error) { return nil, nil }
func (exampleExecutionObserver) Capabilities() Capabilities                    { return Capabilities{Logs: true} }
func (a exampleExecutionObserver) Logs(_ context.Context, ref ExecutionRef, _ LogOptions, emit EmitLog) error {
	if ref.Runtime != a.id {
		return fmt.Errorf("execution belongs to runtime %s", ref.Runtime)
	}
	return emit(LogEntry{Source: ref.Name, Message: "running"})
}

func TestExecutionObservationIsRuntimeNeutral(t *testing.T) {
	for _, id := range []ID{Orka, "kagent"} {
		var observer ExecutionObserver = exampleExecutionObserver{id: id}
		var messages []string
		err := observer.Logs(context.Background(), ExecutionRef{
			Runtime: id, Namespace: "agents", Name: "execution-1", UID: "uid-1",
		}, LogOptions{Follow: true, Tail: 20}, func(entry LogEntry) error {
			messages = append(messages, entry.Message)
			return nil
		})
		if err != nil {
			t.Fatalf("%s Logs: %v", id, err)
		}
		if len(messages) != 1 || messages[0] != "running" {
			t.Fatalf("%s log stream = %v", id, messages)
		}
	}
}
