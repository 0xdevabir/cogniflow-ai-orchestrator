package obs

import (
	"context"
	"errors"
	"testing"
)

func TestInit_None(t *testing.T) {
	shutdown, err := Init(context.Background(), "cogniflow-test", "none")
	if err != nil {
		t.Fatal(err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestInit_Stdout(t *testing.T) {
	shutdown, err := Init(context.Background(), "cogniflow-test", "stdout")
	if err != nil {
		t.Fatal(err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	// Don't actually shut down — the stdout exporter writes to a real fd
	// during Init's New(); we just exercise the code path.
}

func TestStart_ReturnsSpan(t *testing.T) {
	ctx, span := Start(context.Background(), "test.span",
		AttrStr("k", "v"),
	)
	if span == nil {
		t.Fatal("expected span")
	}
	span.End()
	if ctx == nil {
		t.Fatal("expected ctx")
	}
}

func TestRecordError_NilIsNoOp(t *testing.T) {
	_, span := Start(context.Background(), "test.noerr")
	// Recast span into something we can call SetStatus on; we just want to
	// confirm RecordError(nil) doesn't blow up.
	if err := RecordError(span, nil); err != nil {
		t.Fatalf("nil should not return error: %v", err)
	}
	span.End()
}

func TestRecordError_PreservesErr(t *testing.T) {
	_, span := Start(context.Background(), "test.err")
	errBoom := errors.New("boom")
	if got := RecordError(span, errBoom); got != errBoom {
		t.Fatalf("RecordError should pass through, got %v", got)
	}
	span.End()
}

func TestAttrHelpers(t *testing.T) {
	if AttrStr("a", "b").Key != "a" {
		t.Fatal("AttrStr key mismatch")
	}
	if AttrInt("n", 5).Value.AsInt64() != 5 {
		t.Fatal("AttrInt value mismatch")
	}
	if AttrFloat("f", 1.5).Value.AsFloat64() != 1.5 {
		t.Fatal("AttrFloat value mismatch")
	}
	if !AttrBool("b", true).Value.AsBool() {
		t.Fatal("AttrBool value mismatch")
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("OBS_TEST_KEY", "set")
	if got := envOr("OBS_TEST_KEY", "default"); got != "set" {
		t.Errorf("got %q, want %q", got, "set")
	}
	if got := envOr("OBS_TEST_MISSING", "fallback"); got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}
