package sling

import (
	"context"
	"io"
)

type Tracer interface {
	BeginTrace(ctx context.Context)
	EndTrace(ctx context.Context)
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
		b.endTrace()
	}

	return n, err
}

func (b *bodyWithTracer) Close() error {
	err := b.body.Close()
	if err != nil {
		b.endTrace()
	}

	return err
}

func (b *bodyWithTracer) endTrace() {
	if b.hasEnded {
		return
	}

	b.hasEnded = true
	b.tracer.EndTrace(b.ctx)
}
