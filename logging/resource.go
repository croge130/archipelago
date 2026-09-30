package logging

import (
	"log/slog"
	"os"
)

// Resource describes what produced a log line — attached once per
// logger instance, not per event — as distinct from a trace/span, which
// describes which logical operation the line belongs to. Field names
// follow OpenTelemetry's resource semantic conventions so Archipelago's
// logs carry the same attribute names any OTel-aware tooling expects.
type Resource struct {
	// ServiceName is the logical app/service identity — what
	// Archipelago's own design docs call "app".
	ServiceName string
	// ServiceInstanceID is the specific running copy of that service —
	// what the design docs call "instance".
	ServiceInstanceID string
	// ProcessPID is the OS process backing this instance right now.
	// Distinguishes "same instance, but this is run #3 after a
	// restart" when ServiceInstanceID stays stable across restarts.
	ProcessPID int
	// HostName is the machine this process runs on. Left as a plain
	// hostname rather than a stable cross-reboot machine ID; callers
	// with a real machine ID available (e.g. /etc/machine-id) can set
	// it directly instead.
	HostName string
}

// NewResource builds a Resource for the current process, filling
// ProcessPID and HostName automatically. serviceName and instanceID are
// the two fields nothing but the caller can know.
func NewResource(serviceName, instanceID string) Resource {
	host, _ := os.Hostname() // best-effort; empty is fine, never fatal
	return Resource{
		ServiceName:       serviceName,
		ServiceInstanceID: instanceID,
		ProcessPID:        os.Getpid(),
		HostName:          host,
	}
}

// Attrs renders the Resource as slog attributes, suitable for attaching
// to a Logger once via Logger.With so every record it produces carries
// them.
func (r Resource) Attrs() []any {
	attrs := []any{
		slog.String("service.name", r.ServiceName),
		slog.String("service.instance.id", r.ServiceInstanceID),
		slog.Int("process.pid", r.ProcessPID),
	}
	if r.HostName != "" {
		attrs = append(attrs, slog.String("host.name", r.HostName))
	}
	return attrs
}
