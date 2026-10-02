package pulsar

import "testing"

func TestPublisherIDsUseUrdPrefix(t *testing.T) {
	if got := (&EventsPublisher{}).ID(); got != "urd-pulsar" {
		t.Errorf("EventsPublisher.ID() = %q, want %q", got, "urd-pulsar")
	}
	if got := (&DurableStatePublisher{}).ID(); got != "urd-pulsar" {
		t.Errorf("DurableStatePublisher.ID() = %q, want %q", got, "urd-pulsar")
	}
}
