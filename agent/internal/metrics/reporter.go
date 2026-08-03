package metrics

import (
	"log/slog"
	"sync"
)

// WSSender sends JSON messages to the control plane.
// Implemented by *ws.Client.SendJSON.
type WSSender interface {
	SendJSON(v interface{}) error
}

// Reporter sends health reports to the control plane and keeps a small
// bounded ring buffer of recent snapshots for offline batch delivery.
type Reporter struct {
	sender WSSender

	mu     sync.Mutex
	buffer []HealthReport // ring buffer of recent reports
	next   int            // next write index
	count  int            // number of valid entries
}

// NewReporter creates a Reporter with a bounded in-memory ring buffer of
// the given capacity (use 0 to disable buffering).
func NewReporter(sender WSSender, capacity int) *Reporter {
	if capacity < 0 {
		capacity = 0
	}
	return &Reporter{
		sender: sender,
		buffer: make([]HealthReport, capacity),
	}
}

// DefaultBufferCapacity is the number of recent reports kept in memory for
// offline catch-up (100 reports ≈ 50 minutes at the default 30s interval).
func DefaultBufferCapacity() int { return bufferCapacity }

// Send stores the report in the ring buffer and, if connected, delivers it
// to the control plane as a single "health_report" message.
func (r *Reporter) Send(report HealthReport) {
	// Always buffer (bounded).
	r.mu.Lock()
	if len(r.buffer) > 0 {
		r.buffer[r.next] = report
		r.next = (r.next + 1) % len(r.buffer)
		if r.count < len(r.buffer) {
			r.count++
		}
	}
	r.mu.Unlock()

	if r.sender == nil {
		return
	}
	if err := r.sender.SendJSON(report); err != nil {
		slog.Warn("metrics: failed to send health_report", "error", err)
	}
}

// Recent returns the most recent buffered reports in chronological order.
// When offline, callers may use this to send a batched catch-up.
func (r *Reporter) Recent(n int) []HealthReport {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n <= 0 || r.count == 0 {
		return nil
	}
	if n > r.count {
		n = r.count
	}

	out := make([]HealthReport, 0, n)
	// Read newest-first from the ring, then reverse to chronological order.
	idx := (r.next - 1 + len(r.buffer)) % len(r.buffer)
	for i := 0; i < n; i++ {
		out = append(out, r.buffer[idx])
		idx = (idx - 1 + len(r.buffer)) % len(r.buffer)
	}
	// Reverse.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// BufferCount returns how many reports are currently buffered in memory.
func (r *Reporter) BufferCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// SendBatch delivers a batch of reports to the control plane as a
// "health_report_batch" message. Intended for offline catch-up on reconnect.
func (r *Reporter) SendBatch(serverID string, reports []HealthReport) {
	if len(reports) == 0 || r.sender == nil {
		return
	}
	msg := map[string]interface{}{
		"type":     "health_report_batch",
		"server_id": serverID,
		"reports":  reports,
	}
	if err := r.sender.SendJSON(msg); err != nil {
		slog.Warn("metrics: failed to send health_report_batch", "error", err)
	}
}
