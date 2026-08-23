package eventbus

import (
	"fmt"
	"go-odtbank/internal/domain"
	"sync"
)

type EventBus struct {
	subscribers []func(domain.TransferCompletedEvent)
	mu          sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: []func(domain.TransferCompletedEvent){},
	}
}

func (eb *EventBus) Subscribe(handler func(domain.TransferCompletedEvent)) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.subscribers = append(eb.subscribers, handler)
}

func (eb *EventBus) Publish(event domain.TransferCompletedEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, handler := range eb.subscribers {
		go handler(event)
	}
	fmt.Printf("[EventBus] Published TransferCompletedEvent: %v\n", event)
}
