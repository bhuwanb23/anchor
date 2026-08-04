package metrics

import (
	"encoding/json"
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

// rawCaptureSender records every marshaled message sent through SendJSON.
type rawCaptureSender struct {
	msgs []json.RawMessage
}

func (s *rawCaptureSender) SendJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.msgs = append(s.msgs, b)
	return nil
}

func TestReporter_SendBatchFormat(t *testing.T) {
	s := &rawCaptureSender{}
	r := NewReporter(s, 4)

	r.SendBatch("srv-test", []HealthReport{mkReport(1), mkReport(2)})

	if len(s.msgs) != 1 {
		t.Fatalf("sender received %d messages, want 1", len(s.msgs))
	}
	var m map[string]interface{}
	if err := json.Unmarshal(s.msgs[0], &m); err != nil {
		t.Fatalf("unmarshal batch: %v", err)
	}
	if m["type"] != "health_report_batch" {
		t.Errorf("batch type = %v, want health_report_batch", m["type"])
	}
	if m["server_id"] != "srv-test" {
		t.Errorf("batch server_id = %v, want srv-test", m["server_id"])
	}
	arr, ok := m["reports"].([]interface{})
	if !ok || len(arr) != 2 {
		t.Errorf("batch reports = %v, want 2 entries in order", m["reports"])
	}
}

func TestReporter_SendBatchEmpty(t *testing.T) {
	s := &rawCaptureSender{}
	r := NewReporter(s, 4)

	r.SendBatch("srv-test", nil)
	r.SendBatch("srv-test", []HealthReport{})

	if len(s.msgs) != 0 {
		t.Fatalf("expected no message for empty batches, got %d", len(s.msgs))
	}
}
