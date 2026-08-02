package caddy

import (
	"testing"
	"time"
)

func TestEventRecorder_Record(t *testing.T) {
	dir := t.TempDir()
	recorder := NewEventRecorder(dir)

	recorder.Record(ServerEvent{
		Type:    "cert_issued",
		Domain:  "example.com",
		Message: "certificate obtained",
	})

	if recorder.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", recorder.Count())
	}

	events := recorder.GetAll()
	if events[0].Type != "cert_issued" {
		t.Errorf("expected type cert_issued, got %s", events[0].Type)
	}
	if events[0].Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", events[0].Domain)
	}
	if events[0].Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestEventRecorder_GetRecent(t *testing.T) {
	dir := t.TempDir()
	recorder := NewEventRecorder(dir)

	for i := 0; i < 5; i++ {
		recorder.Record(ServerEvent{
			Type:   "cert_issued",
			Domain: "example.com",
		})
	}

	recent := recorder.GetRecent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent events, got %d", len(recent))
	}
}

func TestEventRecorder_GetRecent_ExceedsCount(t *testing.T) {
	dir := t.TempDir()
	recorder := NewEventRecorder(dir)

	recorder.Record(ServerEvent{Type: "cert_issued", Domain: "a.com"})

	recent := recorder.GetRecent(10)
	if len(recent) != 1 {
		t.Fatalf("expected 1 event, got %d", len(recent))
	}
}

func TestEventRecorder_Persistence(t *testing.T) {
	dir := t.TempDir()

	recorder1 := NewEventRecorder(dir)
	recorder1.Record(ServerEvent{Type: "cert_issued", Domain: "persist.com", Message: "test"})
	recorder1.Record(ServerEvent{Type: "cert_renewed", Domain: "persist.com", Message: "renewed"})

	recorder2 := NewEventRecorder(dir)
	if recorder2.Count() != 2 {
		t.Fatalf("expected 2 events after reload, got %d", recorder2.Count())
	}

	events := recorder2.GetAll()
	if events[0].Type != "cert_issued" {
		t.Errorf("expected first event type cert_issued, got %s", events[0].Type)
	}
	if events[1].Type != "cert_renewed" {
		t.Errorf("expected second event type cert_renewed, got %s", events[1].Type)
	}
}

func TestEventRecorder_Rollback(t *testing.T) {
	dir := t.TempDir()
	recorder := NewEventRecorder(dir)

	// Add more than maxStoredEvents
	for i := 0; i < maxStoredEvents+50; i++ {
		recorder.Record(ServerEvent{
			Type:   "cert_issued",
			Domain: "example.com",
		})
	}

	if recorder.Count() != maxStoredEvents+50-eventRollbackBatch {
		t.Errorf("expected %d events after rollback, got %d",
			maxStoredEvents+50-eventRollbackBatch, recorder.Count())
	}
}

func TestEventRecorder_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	recorder := NewEventRecorder(dir)

	if recorder.Count() != 0 {
		t.Errorf("expected 0 events in empty dir, got %d", recorder.Count())
	}

	events := recorder.GetRecent(5)
	if len(events) != 0 {
		t.Errorf("expected 0 recent events, got %d", len(events))
	}
}

func TestEventRecorder_TimestampAutoFill(t *testing.T) {
	dir := t.TempDir()
	recorder := NewEventRecorder(dir)

	before := time.Now()
	recorder.Record(ServerEvent{Type: "cert_issued", Domain: "t.com"})
	after := time.Now()

	events := recorder.GetAll()
	if events[0].Timestamp.Before(before) || events[0].Timestamp.After(after) {
		t.Error("timestamp should be between before and after")
	}
}
