package metrics

import (
	"testing"
	"time"
)

type captureSender struct {
	got []HealthReport
}

func (s *captureSender) SendJSON(v interface{}) error {
	r, ok := v.(HealthReport)
	if ok {
		s.got = append(s.got, r)
	}
	return nil
}

func mkReport(i int) HealthReport {
	return HealthReport{
		Type:      "health_report",
		ServerID:  "srv-test",
		Timestamp: time.Unix(int64(i), 0).UTC(),
	}
}

func TestReporterRingBuffer_BoundedCapacity(t *testing.T) {
	sender := &captureSender{}
	r := NewReporter(sender, 4)

	for i := 0; i < 10; i++ {
		r.Send(mkReport(i))
	}

	if got := r.BufferCount(); got != 4 {
		t.Fatalf("BufferCount = %d, want 4", got)
	}
	// Newest 4 should be 6,7,8,9 in chronological order.
	recent := r.Recent(10)
	if len(recent) != 4 {
		t.Fatalf("Recent(10) = %d entries, want 4", len(recent))
	}
	for i, rep := range recent {
		want := int64(6 + i)
		if got := rep.Timestamp.Unix(); got != want {
			t.Errorf("recent[%d].ts = %d, want %d", i, got, want)
		}
	}
}

func TestReporterRingBuffer_RecentLimit(t *testing.T) {
	sender := &captureSender{}
	r := NewReporter(sender, 8)

	for i := 0; i < 8; i++ {
		r.Send(mkReport(i))
	}

	recent := r.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("Recent(3) = %d entries, want 3", len(recent))
	}
	for i, rep := range recent {
		want := int64(5 + i)
		if got := rep.Timestamp.Unix(); got != want {
			t.Errorf("recent[%d].ts = %d, want %d", i, got, want)
		}
	}
}

func TestReporterRingBuffer_NoCapacity(t *testing.T) {
	r := NewReporter(&captureSender{}, 0)
	r.Send(mkReport(1))
	if got := r.BufferCount(); got != 0 {
		t.Fatalf("BufferCount = %d, want 0 (disabled)", got)
	}
	if got := r.Recent(10); got != nil {
		t.Fatalf("Recent with disabled buffer = %v, want nil", got)
	}
}

func TestReporter_SendsWhenConnected(t *testing.T) {
	sender := &captureSender{}
	r := NewReporter(sender, 4)
	rep := mkReport(1)
	r.Send(rep)
	if len(sender.got) != 1 {
		t.Fatalf("sender received %d reports, want 1", len(sender.got))
	}
}

// offlineSender simulates a disconnected WS client that buffers nothing.
type offlineSender struct{}

func (offlineSender) SendJSON(interface{}) error { return nil }

func TestReporter_RecentEmptyWhenNothingBuffered(t *testing.T) {
	r := NewReporter(offlineSender{}, 4)
	if got := r.Recent(5); got != nil {
		t.Fatalf("Recent on empty buffer = %v, want nil", got)
	}
}
