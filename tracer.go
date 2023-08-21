package sling

import (
	"context"
	"fmt"
	"io"
)

type Tracer interface {
	BeginTrace(ctx context.Context) error
	EndTrace(ctx context.Context) error
}

type bodyWithTracer struct {
	ctx      context.Context
	body     io.ReadCloser
	tracer   Tracer
	hasEnded bool
}

func (b *bodyWithTracer) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err == io.EOF {
		err := b.endTrace()
		if err != nil {
			return 0, fmt.Errorf("got io.EOF and then error while trying to end trace: %w", err)
		}
	}

	return n, err
}

func (b *bodyWithTracer) Close() error {
	err := b.body.Close()
	if err == nil {
		err := b.endTrace()
		if err != nil {
			return fmt.Errorf("got error while trying to end trace: %w", err)
		}
	}

	return err
}

func (b *bodyWithTracer) endTrace() error {
	if b.hasEnded {
		return nil
	}

	b.hasEnded = true
	return b.tracer.EndTrace(b.ctx)
}
