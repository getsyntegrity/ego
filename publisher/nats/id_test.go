package nats

import "testing"

func TestPublisherIDsUseUrdPrefix(t *testing.T) {
	if got := (&EventsPublisher{}).ID(); got != "urd-nats" {
		t.Errorf("EventsPublisher.ID() = %q, want %q", got, "urd-nats")
	}
	if got := (&DurableStatePublisher{}).ID(); got != "urd-nats" {
		t.Errorf("DurableStatePublisher.ID() = %q, want %q", got, "urd-nats")
	}
}
