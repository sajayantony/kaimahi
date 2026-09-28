package runtime

import "context"

// ExecutionRef is the immutable identity of one runtime execution. Name and
// UID are opaque to KMX: an adapter may map them to a Task, run, session, Job,
// or another native execution object without exposing that object model.
type ExecutionRef struct {
	Runtime                       ID
	Context, Namespace, Name, UID string
}

type LogOptions struct {
	Follow bool
	Tail   int
}

type LogEntry struct {
	Source, Message string
}

type EmitLog func(LogEntry) error

// ExecutionObserver is the optional execution-observation sibling of the
// lifecycle and chat contracts. It deliberately does not prescribe how a
// runtime locates logs or what native object represents an execution.
type ExecutionObserver interface {
	Adapter
	Capabilities() Capabilities
	Logs(context.Context, ExecutionRef, LogOptions, EmitLog) error
}
