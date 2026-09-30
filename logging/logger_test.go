package logging

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewTextLoggerIncludesResourceAndCustomLevel(t *testing.T) {
	var buf bytes.Buffer
	resource := Resource{ServiceName: "gatehouse-core", ServiceInstanceID: "i1", ProcessPID: 42}
	logger := NewTextLogger(&buf, resource, LevelTrace)

	logger.Log(context.Background(), LevelNotice, "hello")

	out := buf.String()
	if !strings.Contains(out, "NOTICE") {
		t.Errorf("expected custom level name NOTICE in output, got: %s", out)
	}
	if !strings.Contains(out, "service.name=gatehouse-core") {
		t.Errorf("expected resource attribute in output, got: %s", out)
	}
}

func TestNewTextLoggerRespectsMinLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTextLogger(&buf, Resource{ServiceName: "app"}, LevelInfo)

	logger.Log(context.Background(), LevelDebug, "should be filtered out")

	if buf.Len() != 0 {
		t.Errorf("expected debug below minLevel=Info to be filtered, got: %s", buf.String())
	}
}

func TestNewJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, Resource{ServiceName: "app"}, LevelTrace)
	logger.Log(context.Background(), LevelCritical, "uh oh")

	out := buf.String()
	if !strings.Contains(out, `"CRITICAL"`) {
		t.Errorf("expected JSON output with CRITICAL level, got: %s", out)
	}
}

func TestLogAttachesSpanContextFromContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTextLogger(&buf, Resource{ServiceName: "app"}, LevelTrace)
	sc := NewRootSpan()
	ctx := ContextWithSpan(context.Background(), sc)

	Log(ctx, logger, LevelInfo, "traced event")

	out := buf.String()
	if !strings.Contains(out, "trace_id="+sc.TraceID.String()) {
		t.Errorf("expected trace_id in output, got: %s", out)
	}
	if !strings.Contains(out, "span_id="+sc.SpanID.String()) {
		t.Errorf("expected span_id in output, got: %s", out)
	}
}

func TestLogWithoutSpanContextStillWorks(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTextLogger(&buf, Resource{ServiceName: "app"}, LevelTrace)

	Log(context.Background(), logger, LevelInfo, "untraced event")

	if !strings.Contains(buf.String(), "untraced event") {
		t.Errorf("expected message in output even without a span, got: %s", buf.String())
	}
}
