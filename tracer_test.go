package sling

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestBodyWithTracer(t *testing.T) {

	t.Run("end trace on read finished", func(t *testing.T) {
		tracer, bodyWithTracer := getTestBodyWithTracer(t)

		_, err := io.ReadAll(bodyWithTracer)
		if err != nil {
			t.Errorf("unexpected error reading rest of body: %v", err)
		}

		if len(tracer.calls) != 1 || tracer.calls[0] != "endTrace" {
			t.Errorf("tracer should have been called once to end the trace but had call(s): %v", tracer.calls)
		}
	})

	t.Run("end trace on body close", func(t *testing.T) {
		tracer, bodyWithTracer := getTestBodyWithTracer(t)

		err := bodyWithTracer.Close()
		if err != nil {
			t.Errorf("unexpected error closing body: %v", err)
		}

		if len(tracer.calls) != 1 || tracer.calls[0] != "endTrace" {
			t.Errorf("tracer should have been called once to end the trace but had call(s): %v", tracer.calls)
		}
	})
}

func getTestBodyWithTracer(t *testing.T) (*tracerAndCalls, *bodyWithTracer) {
	tracer := newTestTracer()

	bodyWithTracer := &bodyWithTracer{context.Background(), io.NopCloser(bytes.NewBuffer([]byte("foo bar"))), tracer, false}

	if len(tracer.calls) != 0 {
		t.Errorf("tracer should not be called yet but had call(s): %+v", tracer.calls)
	}

	_, err := io.ReadFull(bodyWithTracer, make([]byte, 3))
	if err != nil {
		t.Errorf("unexpected error reading first body: %v", err)
	}

	if len(tracer.calls) != 0 {
		t.Errorf("tracer should not be called after a partial read but had call(s): %+v", tracer.calls)
	}

	return tracer, bodyWithTracer
}
