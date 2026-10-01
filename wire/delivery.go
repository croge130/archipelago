package wire

// DeliveryClass is the Session-level intent a caller declares — the
// backend picks the mechanism, never the caller, per
// 11-transit-model.md's own reasoning: a SendDatagram()-style method
// would be lossy on one backend and lossless on another, so intent
// has to be named independently of mechanism for a transport-agnostic
// caller to be written correctly at all.
//
// Every class is contracted at its weakest guarantee. Signal is "may
// be dropped, may arrive out of order" even on a backend (WebSocket)
// that happens to over-deliver it — strengthening a guarantee never
// breaks a correct consumer, which is what lets a reliable-only
// backend be built first without painting a future lossy one into a
// corner.
type DeliveryClass string

const (
	// RequestResponse is ordered per-id, reliable, frame-capped — an
	// inline correlated reply on WebSocket, a bidi/control stream on
	// WebTransport.
	RequestResponse DeliveryClass = "request_response"

	// EventPush is ordered per-channel, reliable, frame-capped — a
	// reliable non-blocking push on WebSocket, a unidirectional
	// stream on WebTransport.
	EventPush DeliveryClass = "event_push"

	// ChannelStream is ordered per-channel, reliable, streamed — a
	// logical mux channel on WebSocket, a native QUIC stream on
	// WebTransport.
	ChannelStream DeliveryClass = "channel_stream"

	// Signal is fire-and-forget: no ordering, NOT reliable, small —
	// deferred (no current consumer wants lossy delivery; see
	// 11-transit-model.md). Kept in the vocabulary so it slots in
	// later without reworking this type.
	Signal DeliveryClass = "signal"
)

func (c DeliveryClass) Valid() bool {
	switch c {
	case RequestResponse, EventPush, ChannelStream, Signal:
		return true
	default:
		return false
	}
}

// Reliable reports whether c guarantees delivery — false only for
// Signal.
func (c DeliveryClass) Reliable() bool {
	return c != Signal
}

// Ordered reports whether c guarantees ordering — false only for
// Signal.
func (c DeliveryClass) Ordered() bool {
	return c != Signal
}
