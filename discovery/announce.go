package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/hashicorp/mdns"
)

// Service is the DNS-SD service type announced and browsed for.
const Service = "_archipelago._tcp"

// protoKey is the one TXT key an announcement carries. Its value is
// "<min>-<max>", the protocol versions the announcer speaks (the same range
// the router's handshake negotiates). Unknown keys from a newer peer are
// ignored on receipt.
const protoKey = "proto"

// Announcement describes what one listener announces. A node that listens
// per domain announces once per listener; nothing in it says which domain.
type Announcement struct {
	// Port is the listener's port.
	Port int
	// MinProtocol and MaxProtocol are the protocol versions it speaks.
	MinProtocol, MaxProtocol int

	// Addrs overrides the addresses announced (default: the machine's
	// multicast-capable interface addresses, or loopback if there are none).
	Addrs []net.IP
	// Interface restricts multicast to one interface (default: the system's).
	Interface *net.Interface

	Logger *slog.Logger
}

// Announcer is a running announcement.
type Announcer struct {
	srv      *mdns.Server
	instance string
}

// Announce starts answering queries for Service with this announcement.
// The instance name is random per announcement, so it identifies nothing
// about the node or its domains.
func Announce(a Announcement) (*Announcer, error) {
	switch {
	case a.Port < 1 || a.Port > 65535:
		return nil, fmt.Errorf("discovery: port %d is out of range", a.Port)
	case a.MinProtocol < 1 || a.MaxProtocol < a.MinProtocol || a.MaxProtocol > 65535:
		return nil, fmt.Errorf("discovery: protocol range %d-%d is invalid", a.MinProtocol, a.MaxProtocol)
	}
	addrs := a.Addrs
	if len(addrs) == 0 {
		addrs = localAddrs()
	}
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("discovery: instance name: %w", err)
	}
	instance := "archipelago-" + hex.EncodeToString(raw[:])
	txt := []string{fmt.Sprintf("%s=%d-%d", protoKey, a.MinProtocol, a.MaxProtocol)}
	svc, err := mdns.NewMDNSService(instance, Service, "", instance+".local.", a.Port, addrs, txt)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	srv, err := mdns.NewServer(&mdns.Config{Zone: svc, Iface: a.Interface, Logger: stdLogger(a.Logger)})
	if err != nil {
		return nil, fmt.Errorf("discovery: start announcing: %w", err)
	}
	return &Announcer{srv: srv, instance: instance}, nil
}

// Instance is the announced instance name, so a node can recognise and skip
// its own announcement when browsing.
func (a *Announcer) Instance() string { return a.instance }

// Close stops answering. mDNS has no registry to deregister from: a browse
// that starts afterwards simply gets no answer from this announcer.
func (a *Announcer) Close() error {
	if a == nil || a.srv == nil {
		return errors.New("discovery: not announcing")
	}
	return a.srv.Shutdown()
}
