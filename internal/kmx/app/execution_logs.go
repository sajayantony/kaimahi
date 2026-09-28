package app

import (
	"context"
	"fmt"

	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

func (a *App) tailExecutionLogs(ctx context.Context, observer agentruntime.ExecutionObserver, ref agentruntime.ExecutionRef) error {
	if !observer.Capabilities().Logs {
		return &agentruntime.UnsupportedVerbError{Runtime: observer.ID(), Verb: agentruntime.VerbLogs}
	}
	a.notef("TAIL  execution %s logs", ref.Name)
	return observer.Logs(ctx, ref, agentruntime.LogOptions{Follow: true, Tail: 20}, func(entry agentruntime.LogEntry) error {
		_, err := fmt.Fprintln(a.Err, entry.Message)
		return err
	})
}
