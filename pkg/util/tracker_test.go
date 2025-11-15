package util

import (
	"testing"
	"time"
)

func TestNamespaceTracker_BasicOperations(t *testing.T) {
	t.Parallel()

	tr := NewNamespaceTracker()

	// Initially empty
	if len(tr.ListNamespaces()) != 0 {
		t.Fatalf("expected empty namespace list, got %v", tr.ListNamespaces())
	}

	// Add and check TrackingNamespace
	tr.AddNamespace("ns1")
	if !tr.TrackingNamespace("ns1") {
		t.Fatalf("expected ns1 to be tracked")
	}

	// Add duplicate should be idempotent
	tr.AddNamespace("ns1")
	list := tr.ListNamespaces()
	if len(list) != 1 {
		t.Fatalf("duplicate namespaces resulted in %d items, want 1", len(list))
	}

	// Remove and check
	tr.RemoveNamespace("ns1")
	if tr.TrackingNamespace("ns1") {
		t.Fatalf("expected ns1 to not be tracked after removal")
	}
}

func TestNamespaceTracker_Listeners(t *testing.T) {
	t.Parallel()

	tr := NewNamespaceTracker()

	// Buffered channel to receive events
	ch := make(chan NamespaceChangeEvent, 10)
	tr.RegisterListener(ch)

	tr.AddNamespace("alpha")
	select {
	case ev := <-ch:
		if ev.Namespace != "alpha" || ev.Change != NamespaceAdded {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected event for add to be sent to listener")
	}

	tr.RemoveNamespace("alpha")
	select {
	case ev := <-ch:
		if ev.Namespace != "alpha" || ev.Change != NamespaceRemoved {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected event for remove to be sent to listener")
	}
}

func TestNamespaceTracker_NonBlockingOnFullListener(t *testing.T) {
	t.Parallel()

	tr := NewNamespaceTracker()

	// Unbuffered channel ensures the listener is not ready - send attempt would block
	ch := make(chan NamespaceChangeEvent)
	tr.RegisterListener(ch)

	done := make(chan struct{})
	go func() {
		tr.AddNamespace("blocked")
		close(done)
	}()

	select {
	case <-done:
		// success - AddNamespace did not block even though listener was not ready
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("AddNamespace blocked while notifying a full/unready listener")
	}
}
