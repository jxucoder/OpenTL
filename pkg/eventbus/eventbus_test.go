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

func TestMultipleSubscribers(t *testing.T) {
	bus := NewInMemoryBus()
	ch1 := bus.Subscribe("s3")
	ch2 := bus.Subscribe("s3")

	ev := &model.Event{SessionID: "s3", Type: "status", Data: "hello"}
	bus.Publish("s3", ev)

	for _, ch := range []chan *model.Event{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Data != "hello" {
				t.Fatalf("unexpected data: %s", got.Data)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("subscriber did not receive event")
		}
	}

	bus.Unsubscribe("s3", ch1)
	bus.Unsubscribe("s3", ch2)
}

func TestPublishToWrongSession(t *testing.T) {
	bus := NewInMemoryBus()
	ch := bus.Subscribe("s4")

	bus.Publish("other-session", &model.Event{SessionID: "other-session", Type: "status", Data: "x"})

	select {
	case <-ch:
		t.Fatal("should not receive event for a different session")
	case <-time.After(100 * time.Millisecond):
		// expected
	}

	bus.Unsubscribe("s4", ch)
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	bus := NewInMemoryBus()
	ch := bus.Subscribe("s5")

	bus.Unsubscribe("s5", ch)

	// Channel should be closed.
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}

func TestSubscribeAfterUnsubscribe(t *testing.T) {
	bus := NewInMemoryBus()
	ch1 := bus.Subscribe("s6")
	bus.Unsubscribe("s6", ch1)

	ch2 := bus.Subscribe("s6")
	ev := &model.Event{SessionID: "s6", Type: "output", Data: "new"}
	bus.Publish("s6", ev)

	select {
	case got := <-ch2:
		if got.Data != "new" {
			t.Fatalf("unexpected data: %s", got.Data)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("new subscriber did not receive event")
	}

	bus.Unsubscribe("s6", ch2)
}
