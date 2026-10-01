package wire

import "testing"

func TestNewChannelIDCarriesInitiator(t *testing.T) {
	clientID := NewChannelID(InitiatorClient)
	initiator, ok := ChannelInitiator(clientID)
	if !ok || initiator != InitiatorClient {
		t.Fatalf("ChannelInitiator(%q) = (%q, %v), want (client, true)", clientID, initiator, ok)
	}

	serverID := NewChannelID(InitiatorServer)
	initiator, ok = ChannelInitiator(serverID)
	if !ok || initiator != InitiatorServer {
		t.Fatalf("ChannelInitiator(%q) = (%q, %v), want (server, true)", serverID, initiator, ok)
	}
}

func TestNewChannelIDNamespacesNeverCollide(t *testing.T) {
	client := NewChannelID(InitiatorClient)
	server := NewChannelID(InitiatorServer)
	if client == server {
		t.Fatal("expected client- and server-allocated channel ids to never collide")
	}
}

func TestChannelInitiatorRejectsMalformedID(t *testing.T) {
	if _, ok := ChannelInitiator("not-namespaced"); ok {
		t.Fatal("expected a channel id with no initiator prefix to be rejected")
	}
	if _, ok := ChannelInitiator("alien:abc123"); ok {
		t.Fatal("expected an unknown initiator prefix to be rejected")
	}
}

func TestInitiatorValid(t *testing.T) {
	if !InitiatorClient.Valid() || !InitiatorServer.Valid() {
		t.Fatal("expected both Initiator values to be valid")
	}
	if Initiator("alien").Valid() {
		t.Fatal("expected an unknown Initiator to be invalid")
	}
}
