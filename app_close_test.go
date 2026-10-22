package main

import (
	"context"
	"gytest-go/internal/core"
	"testing"
)

func TestDirtyApplicationClosesWithoutConfirmationAndStopsChannels(t *testing.T) {
	a := NewApp()
	id, err := a.engine.UpsertChannel(core.Channel{Name: "waiting", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: "192.0.2.1:2425", TimeoutMS: 30000})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.engine.Start(id); err != nil {
		t.Fatal(err)
	}
	defer a.engine.StopAll()
	if !a.engine.Snapshot().Dirty {
		t.Fatal("fixture must be unsaved")
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("main close was intercepted")
	}
	a.shutdown(context.Background())
	for _, s := range a.engine.Snapshot().States {
		if s.Status != "stopped" {
			t.Fatal("shutdown left an active channel")
		}
	}
}
