package kafka

import "testing"

func TestPublisherIDsUseUrdPrefix(t *testing.T) {
	if got := (&EventsPublisher{}).ID(); got != "urd-kafka" {
		t.Errorf("EventsPublisher.ID() = %q, want %q", got, "urd-kafka")
	}
	if got := (&DurableStatePublisher{}).ID(); got != "urd-kafka" {
		t.Errorf("DurableStatePublisher.ID() = %q, want %q", got, "urd-kafka")
	}
}

func TestSaramaClientIDUsesUrdPrefix(t *testing.T) {
	if got := toSaramaConfig(&Config{}).ClientID; got != "urd-kafka-publisher" {
		t.Errorf("ClientID = %q, want %q", got, "urd-kafka-publisher")
	}
}
