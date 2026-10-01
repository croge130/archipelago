package wire

import "testing"

func TestDeliveryClassValid(t *testing.T) {
	for _, c := range []DeliveryClass{RequestResponse, EventPush, ChannelStream, Signal} {
		if !c.Valid() {
			t.Fatalf("expected %q to be valid", c)
		}
	}
	if DeliveryClass("nonsense").Valid() {
		t.Fatal("expected an unknown DeliveryClass to be invalid")
	}
}

func TestOnlySignalIsUnreliableAndUnordered(t *testing.T) {
	for _, c := range []DeliveryClass{RequestResponse, EventPush, ChannelStream} {
		if !c.Reliable() || !c.Ordered() {
			t.Errorf("expected %q to be reliable and ordered", c)
		}
	}
	if Signal.Reliable() || Signal.Ordered() {
		t.Fatal("expected Signal to be neither reliable nor ordered — it's contracted at its weakest guarantee")
	}
}

func TestKindValid(t *testing.T) {
	known := []Kind{KindRequest, KindResponse, KindEvent, KindAck,
		KindStreamOpen, KindStreamData, KindStreamEnd, KindCancel, KindError}
	for _, k := range known {
		if !k.Valid() {
			t.Fatalf("expected %q to be valid", k)
		}
	}
	if Kind("nonsense").Valid() {
		t.Fatal("expected an unknown Kind to be invalid")
	}
}
