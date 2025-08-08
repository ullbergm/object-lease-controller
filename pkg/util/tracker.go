package util

import (
	"log"
	"sync"
)

// NamespaceChangeType represents the type of change in the tracker
type NamespaceChangeType int

const (
	NamespaceAdded NamespaceChangeType = iota
	NamespaceRemoved
)

// NamespaceChangeEvent represents a change event for namespaces
type NamespaceChangeEvent struct {
	Namespace string
	Change    NamespaceChangeType
}

// NamespaceTracker tracks namespaces and notifies listeners on changes
type NamespaceTracker struct {
	mu         sync.RWMutex
	namespaces map[string]struct{}
	listeners  []chan NamespaceChangeEvent
}

func NewNamespaceTracker() *NamespaceTracker {
	return &NamespaceTracker{
		namespaces: make(map[string]struct{}),
		listeners:  make([]chan NamespaceChangeEvent, 0),
	}
}

// AddNamespace adds a namespace and notifies listeners
func (t *NamespaceTracker) AddNamespace(ns string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.namespaces[ns]; !exists {
		t.namespaces[ns] = struct{}{}
		t.notifyListeners(NamespaceChangeEvent{Namespace: ns, Change: NamespaceAdded})
	}
}

// RemoveNamespace removes a namespace and notifies listeners
func (t *NamespaceTracker) RemoveNamespace(ns string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.namespaces[ns]; exists {
		delete(t.namespaces, ns)
		t.notifyListeners(NamespaceChangeEvent{Namespace: ns, Change: NamespaceRemoved})
	}
}

// ListNamespaces returns a slice of tracked namespaces
func (t *NamespaceTracker) ListNamespaces() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ns := make([]string, 0, len(t.namespaces))
	for k := range t.namespaces {
		ns = append(ns, k)
	}
	return ns
}

// TrackingNamespace returns true if the namespace is being tracked
func (t *NamespaceTracker) TrackingNamespace(ns string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, exists := t.namespaces[ns]
	return exists
}

// RegisterListener registers a channel to receive change events
func (t *NamespaceTracker) RegisterListener(ch chan NamespaceChangeEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, ch)
}

func (t *NamespaceTracker) notifyListeners(event NamespaceChangeEvent) {
	for _, ch := range t.listeners {
		select {
		case ch <- event:
		default:
			log.Printf("NamespaceTracker: dropped event for namespace %q (change: %v) because listener channel is full", event.Namespace, event.Change)
		}
	}
}
