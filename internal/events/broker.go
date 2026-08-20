package events

import "sync"

// subscriberBuffer is how many events one subscriber may fall behind by.
const subscriberBuffer = 256

// Broker fans out events to everyone watching.
type Broker struct {
	mu          sync.Mutex
	subscribers map[chan Event]struct{}
}

// NewBroker builds an empty broker.
func NewBroker() *Broker {
	return &Broker{subscribers: make(map[chan Event]struct{})}
}

// Subscribe returns a channel of events and the function that ends the
// subscription. The function closes the channel and is safe to call twice.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subscribers, ch)
			close(ch)
		})
	}
	return ch, cancel
}

// Publish hands the event to every subscriber. A subscriber whose buffer is
// full loses the event, because serving a request must never wait on a watcher.
func (b *Broker) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
