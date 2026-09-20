package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/aakash/godrop/internal/pool"
)

func TestProcessorFunc(t *testing.T) {
	var p pool.Processor[string, int] = pool.ProcessorFunc[string, int](
		func(ctx context.Context, s string) (int, error) {
			if s == "" {
				return 0, errors.New("empty")
			}
			return len(s), nil
		},
	)

	n, err := p.Process(context.Background(), "hello")
	if err != nil || n != 5 {
		t.Fatalf("got (%d, %v), want (5, nil)", n, err)
	}

	if _, err := p.Process(context.Background(), ""); err == nil {
		t.Fatal("expected an error for empty input")
	}
}
