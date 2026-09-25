package orchestration

import (
	"context"
	"io"
)

type sliceStream[T any] struct {
	values []T
	index  int
	closed bool
}

func (s *sliceStream[T]) Next(context.Context) (T, error) {
	var zero T
	if s.closed || s.index >= len(s.values) {
		return zero, io.EOF
	}
	value := s.values[s.index]
	s.index++
	return value, nil
}

func (s *sliceStream[T]) Close() error {
	s.closed = true
	return nil
}
