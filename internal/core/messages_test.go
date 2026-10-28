package core

import (
	"sync"
	"testing"
	"time"
)

func TestMessageSubscriptionsNeverBlockProtocolAndKeepDesktopBuffer(t *testing.T) {
	e := NewEngine()
	systems := e.SubscribeMessages(1, false)
	frames := e.SubscribeMessages(1, true)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 3000; i++ {
			e.log("channel", "", "RX", []byte{1, 2}, "packet")
		}
		e.log("channel", "", "SYS", nil, "state")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("slow observer blocked protocol")
	}
	if systems.Dropped() != 0 || frames.Dropped() != 3000 {
		t.Fatal(systems.Dropped(), frames.Dropped())
	}
	if message := <-systems.Messages(); message.Direction != "SYS" {
		t.Fatal(message)
	}
	snap := e.Snapshot()
	if snap.States != nil && len(snap.States) != 0 {
		t.Fatal(snap.States)
	}
	if len(snap.Messages) > 2000 || snap.Messages[len(snap.Messages)-1].Sequence != 3001 {
		t.Fatal("desktop retention changed")
	}
	systems.Close()
	systems.Close()
	frames.Close()
}
func TestSubscriptionCloseConcurrentWithLogging(t *testing.T) {
	e := NewEngine()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			e.log("x", "", "SYS", nil, "state")
		}
	}()
	for i := 0; i < 100; i++ {
		s := e.SubscribeMessages(4, true)
		s.Close()
	}
	wg.Wait()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.subscribers) != 0 {
		t.Fatal("observers leaked")
	}
}
