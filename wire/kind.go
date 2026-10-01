package wire

// Kind is the envelope's own message-shape field — orthogonal to
// DeliveryClass (which describes the Session-level intent a handler
// declared). request/response/event/ack cover ordinary exchanges;
// stream_open/stream_data/stream_end cover a Channel's lifecycle;
// cancel and error are control kinds that can appear in any of those
// contexts.
type Kind string

const (
	KindRequest    Kind = "request"
	KindResponse   Kind = "response"
	KindEvent      Kind = "event"
	KindAck        Kind = "ack"
	KindStreamOpen Kind = "stream_open"
	KindStreamData Kind = "stream_data"
	KindStreamEnd  Kind = "stream_end"
	KindCancel     Kind = "cancel"
	KindError      Kind = "error"
)

func (k Kind) Valid() bool {
	switch k {
	case KindRequest, KindResponse, KindEvent, KindAck,
		KindStreamOpen, KindStreamData, KindStreamEnd, KindCancel, KindError:
		return true
	default:
		return false
	}
}
