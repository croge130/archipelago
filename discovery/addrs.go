package discovery

import (
	"io"
	"log"
	"log/slog"
	"net"
)

// localAddrs are the addresses worth announcing: those of interfaces that
// are up and can multicast, excluding loopback and IPv6 link-local (a
// link-local address is meaningless without the zone of the link it was
// heard on, and the receiver supplies that itself). If there are none, the
// loopback address is used so a single machine can still find itself, which
// is what a development setup or a test needs.
func localAddrs() []net.IP {
	var out []net.IP
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP
			switch {
			case ip.To4() != nil:
				out = append(out, ip.To4())
			case ip.IsGlobalUnicast():
				out = append(out, ip)
			}
		}
	}
	if len(out) == 0 {
		out = append(out, net.IPv4(127, 0, 0, 1))
	}
	return out
}

// stdLogger adapts an slog.Logger for the mDNS library, which takes a
// *log.Logger. Its messages are diagnostics, so they go out at debug.
func stdLogger(l *slog.Logger) *log.Logger {
	if l == nil {
		return log.New(io.Discard, "", 0)
	}
	return slog.NewLogLogger(l.Handler(), slog.LevelDebug)
}
