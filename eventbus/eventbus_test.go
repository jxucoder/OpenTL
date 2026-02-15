package eventbus

import (
	"testing"
	"time"

	"github.com/jxucoder/TeleCoder/model"
)

func TestSubscribePublishUnsubscribe(t *testing.T) {
	bus := NewInMemoryBus()
	ch := bus.Subscribe("s1")

	ev := &model.Event{SessionID: "s1", Type: "status", Data: "ok"}
	bus.Publish("s1", ev)

	select {
	case got := <-ch:
		if got.Data != "ok" {
			t.Fatalf("unexpected event data: %s", got.Data)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive event")
	}

	bus.Unsubscribe("s1", ch)
}

func TestDoesNotBlockOnSlowSubscriber(t *testing.T) {
	bus := NewInMemoryBus()
	ch := bus.Subscribe("s2")

	// Fill channel to capacity (64) without reading.
	for i := 0; i < 64; i++ {
		bus.Publish("s2", &model.Event{SessionID: "s2", Type: "output", Data: "x"})
	}

	done := make(chan struct{})
	go func() {
		// This publish should be dropped and return immediately.
		bus.Publish("s2", &model.Event{SessionID: "s2", Type: "output", Data: "overflow"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("publish blocked on full channel")
	}

	bus.Unsubscribe("s2", ch)
}
