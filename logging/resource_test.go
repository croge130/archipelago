package logging

import (
	"log/slog"
	"testing"
)

func TestResourceAttrs(t *testing.T) {
	r := Resource{
		ServiceName:       "gatehouse-core",
		ServiceInstanceID: "instance-1",
		ProcessPID:        1234,
		HostName:          "host-a",
	}
	attrs := r.Attrs()
	want := map[string]string{
		"service.name":        "gatehouse-core",
		"service.instance.id": "instance-1",
		"host.name":           "host-a",
	}
	seen := map[string]bool{}
	for _, a := range attrs {
		attr, ok := a.(slog.Attr)
		if !ok {
			t.Fatalf("Resource.Attrs() element is not a slog.Attr: %v (%T)", a, a)
		}
		seen[attr.Key] = true
		if wantVal, ok := want[attr.Key]; ok && attr.Value.String() != wantVal {
			t.Errorf("attr %s = %q, want %q", attr.Key, attr.Value.String(), wantVal)
		}
		if attr.Key == "process.pid" && attr.Value.Int64() != 1234 {
			t.Errorf("process.pid = %d, want 1234", attr.Value.Int64())
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("Resource.Attrs() missing %s", k)
		}
	}
	if !seen["process.pid"] {
		t.Error("Resource.Attrs() missing process.pid")
	}
}

func TestResourceAttrsOmitsEmptyHostName(t *testing.T) {
	r := Resource{ServiceName: "app", ServiceInstanceID: "i1", ProcessPID: 1}
	for _, a := range r.Attrs() {
		attr := a.(slog.Attr)
		if attr.Key == "host.name" {
			t.Error("Resource.Attrs() included host.name when HostName was empty")
		}
	}
}

func TestNewResourceFillsProcessFields(t *testing.T) {
	r := NewResource("app", "instance-1")
	if r.ProcessPID == 0 {
		t.Error("NewResource left ProcessPID at zero")
	}
	if r.ServiceName != "app" || r.ServiceInstanceID != "instance-1" {
		t.Errorf("NewResource did not preserve caller-supplied fields: %+v", r)
	}
}
