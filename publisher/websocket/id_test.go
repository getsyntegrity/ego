package websocket

import "testing"

func TestPublisherIDsUseUrdPrefix(t *testing.T) {
	if got := (&EventsPublisher{}).ID(); got != "urd-websocket" {
		t.Errorf("EventsPublisher.ID() = %q, want %q", got, "urd-websocket")
	}
	if got := (&DurableStatePublisher{}).ID(); got != "urd-websocket" {
		t.Errorf("DurableStatePublisher.ID() = %q, want %q", got, "urd-websocket")
	}
}
