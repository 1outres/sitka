package events

import (
	"testing"
	"time"
)

func waitFor(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-ch:
		if !ok {
			t.Fatal("the channel closed before an event arrived")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("no event arrived")
		return Event{}
	}
}

func TestEverySubscriberSeesTheSameEvent(t *testing.T) {
	broker := NewBroker()
	first, cancelFirst := broker.Subscribe()
	defer cancelFirst()
	second, cancelSecond := broker.Subscribe()
	defer cancelSecond()

	broker.Publish(Event{Model: "openai-gpt-5.2"})

	for name, ch := range map[string]<-chan Event{"first": first, "second": second} {
		if got := waitFor(t, ch); got.Model != "openai-gpt-5.2" {
			t.Errorf("%s subscriber got model %q, want %q", name, got.Model, "openai-gpt-5.2")
		}
	}
}

func TestCancelClosesTheChannel(t *testing.T) {
	broker := NewBroker()
	ch, cancel := broker.Subscribe()

	cancel()
	broker.Publish(Event{Model: "openai-gpt-5.2"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("a cancelled subscriber still received an event")
		}
	case <-time.After(time.Second):
		t.Error("the channel of a cancelled subscriber stayed open")
	}
}

func TestCancelTwiceIsSafe(_ *testing.T) {
	broker := NewBroker()
	_, cancel := broker.Subscribe()

	cancel()
	cancel()
}

// TestPublishDoesNotWaitForASlowSubscriber proves the gateway keeps serving
// even when nothing reads the watch stream.
func TestPublishDoesNotWaitForASlowSubscriber(t *testing.T) {
	broker := NewBroker()
	_, cancel := broker.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < subscriberBuffer*4; i++ {
			broker.Publish(Event{Model: "openai-gpt-5.2"})
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a subscriber that reads nothing")
	}
}

func TestPublishWithoutSubscribersIsSafe(_ *testing.T) {
	NewBroker().Publish(Event{Model: "openai-gpt-5.2"})
}
