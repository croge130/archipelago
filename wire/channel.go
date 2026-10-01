package wire

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Initiator says which side opened a channel. The party that opens a
// channel allocates its id from its own namespace — unifying "client
// allocates" (an ordinary subscription) and "server allocates" (a
// brokered relay between two peers, where the server is the opener on
// behalf of both) under one rule rather than two separate schemes.
type Initiator string

const (
	InitiatorClient Initiator = "client"
	InitiatorServer Initiator = "server"
)

func (i Initiator) Valid() bool {
	return i == InitiatorClient || i == InitiatorServer
}

// NewChannelID allocates a channel id carrying initiator inside it,
// so the two namespaces never collide even without a central
// allocator.
func NewChannelID(initiator Initiator) string {
	return string(initiator) + ":" + uuid.New().String()
}

// ChannelInitiator reports which side allocated channelID.
func ChannelInitiator(channelID string) (Initiator, bool) {
	prefix, _, found := strings.Cut(channelID, ":")
	if !found {
		return "", false
	}
	initiator := Initiator(prefix)
	if !initiator.Valid() {
		return "", false
	}
	return initiator, true
}

func validateChannelID(channelID string) error {
	if _, ok := ChannelInitiator(channelID); !ok {
		return fmt.Errorf("wire: channel id %q does not carry a valid initiator prefix", channelID)
	}
	return nil
}
