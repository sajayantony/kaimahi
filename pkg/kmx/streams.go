package kmx

import (
	"context"
	"time"
)

type Event struct {
	Sequence uint64    `json:"sequence"`
	Type     string    `json:"type"`
	Message  string    `json:"message,omitempty"`
	Time     time.Time `json:"time"`
}

type LogEntry struct {
	Sequence uint64    `json:"sequence"`
	Level    string    `json:"level"`
	Message  string    `json:"message"`
	Time     time.Time `json:"time"`
}

type Output struct {
	MediaType string `json:"mediaType"`
	Data      []byte `json:"data"`
	Digest    Digest `json:"digest"`
}

type Artifact struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Data      []byte `json:"data"`
	Digest    Digest `json:"digest"`
}

type EventStream interface {
	Next(context.Context) (Event, error)
	Close() error
}

type LogStream interface {
	Next(context.Context) (LogEntry, error)
	Close() error
}

type ArtifactStream interface {
	Next(context.Context) (Artifact, error)
	Close() error
}
