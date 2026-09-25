package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/target"
	"github.com/kaimahi-agents/kaimahi/pkg/kmx"
)

type Target struct {
	mu           sync.Mutex
	facts        target.Facts
	executions   map[string]target.ApplyReceipt
	journal      []string
	now          func() time.Time
	nextRevision uint64
}

func New(ref kmx.TargetRef, labels map[string]string) *Target {
	return &Target{
		facts: target.Facts{
			Ref:    ref,
			Labels: cloneMap(labels),
		},
		executions: make(map[string]target.ApplyReceipt),
		now:        time.Now,
	}
}

func (t *Target) Resolve(_ context.Context, selector kmx.TargetSelector) (target.Facts, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if selector.Class != t.facts.Ref.Class {
		return target.Facts{}, &kmx.NotFoundError{Subject: "target"}
	}
	for key, value := range selector.Match {
		if t.facts.Labels[key] != value {
			return target.Facts{}, &kmx.NotFoundError{Subject: "target"}
		}
	}
	t.journal = append(t.journal, "resolve")
	return target.Facts{Ref: t.facts.Ref, Labels: cloneMap(t.facts.Labels)}, nil
}

func (t *Target) Validate(
	_ context.Context,
	_ target.Facts,
	request target.ValidationRequest,
) (target.ValidationResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if request.ExecutionID == "" || len(request.Resources) == 0 {
		return target.ValidationResult{}, &kmx.InvalidError{
			Field:  "target validation",
			Reason: "requires execution identity and resources",
		}
	}
	for _, resource := range request.Resources {
		if resource.LogicalName == "" || len(resource.Data) == 0 {
			return target.ValidationResult{}, &kmx.InvalidError{
				Field:  "target resource",
				Reason: "requires name and data",
			}
		}
	}
	t.journal = append(t.journal, "validate:"+request.ExecutionID)
	return target.ValidationResult{Accepted: true}, nil
}

func (t *Target) Apply(
	_ context.Context,
	_ target.Facts,
	request target.ApplyRequest,
) (target.ApplyReceipt, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	data, err := json.Marshal(request.Resources)
	if err != nil {
		return target.ApplyReceipt{}, fmt.Errorf("marshal target resources: %w", err)
	}
	t.nextRevision++
	receipt := target.ApplyReceipt{
		ExecutionID: request.ExecutionID,
		Revision:    fmt.Sprintf("r%d", t.nextRevision),
		Digest:      kmx.NewDigest(data),
		AppliedAt:   t.now().UTC(),
	}
	t.executions[request.ExecutionID] = receipt
	t.journal = append(t.journal, "apply:"+request.ExecutionID)
	return receipt, nil
}

func (t *Target) Read(
	_ context.Context,
	_ target.Facts,
	request target.ReadRequest,
) (target.Observation, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	receipt, ok := t.executions[request.ExecutionID]
	t.journal = append(t.journal, "read:"+request.ExecutionID)
	if !ok {
		return target.Observation{
			Found:      false,
			ObservedAt: t.now().UTC(),
		}, nil
	}
	return target.Observation{
		Found:      true,
		Revision:   receipt.Revision,
		Digest:     receipt.Digest,
		ObservedAt: t.now().UTC(),
	}, nil
}

func (t *Target) Watch(
	ctx context.Context,
	facts target.Facts,
	request target.WatchRequest,
) (target.ObservationStream, error) {
	observation, err := t.Read(ctx, facts, target.ReadRequest{ExecutionID: request.ExecutionID})
	if err != nil {
		return nil, err
	}
	return &stream{values: []target.Observation{observation}}, nil
}

func (t *Target) Delete(
	_ context.Context,
	_ target.Facts,
	request target.DeleteRequest,
) (target.DeleteReceipt, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, found := t.executions[request.ExecutionID]
	delete(t.executions, request.ExecutionID)
	t.journal = append(t.journal, "delete:"+request.ExecutionID)
	return target.DeleteReceipt{
		ExecutionID: request.ExecutionID,
		Deleted:     found,
		DeletedAt:   t.now().UTC(),
	}, nil
}

func (t *Target) Journal() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.journal...)
}

type stream struct {
	values []target.Observation
	index  int
	closed bool
}

func (s *stream) Next(context.Context) (target.Observation, error) {
	if s.closed || s.index >= len(s.values) {
		return target.Observation{}, io.EOF
	}
	value := s.values[s.index]
	s.index++
	return value, nil
}

func (s *stream) Close() error {
	s.closed = true
	return nil
}

func cloneMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	cloned := make(map[string]string, len(values))
	for _, key := range keys {
		cloned[key] = values[key]
	}
	return cloned
}
