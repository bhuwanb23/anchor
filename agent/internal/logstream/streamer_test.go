package logstream

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSender records all messages sent via SendJSON.
type mockSender struct {
	mu       sync.Mutex
	messages []interface{}
}

func (s *mockSender) SendJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, v)
	return nil
}

func (s *mockSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *mockSender) getMessages() []interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]interface{}, len(s.messages))
	copy(cp, s.messages)
	return cp
}

func buildTestFrame(streamType int, payload string) []byte {
	data := []byte(payload)
	header := make([]byte, headerSize)
	header[0] = byte(streamType)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(data)))
	return append(header, data...)
}

func TestNewLogStreamer(t *testing.T) {
	sender := &mockSender{}
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "", nil
	}

	ls := NewLogStreamer(opener, fetcher, sender)
	if ls == nil {
		t.Fatal("NewLogStreamer returned nil")
	}
	if ls.ActiveStreams() != 0 {
		t.Errorf("expected 0 active streams, got %d", ls.ActiveStreams())
	}
}

func TestStopStream_NotActive(t *testing.T) {
	sender := &mockSender{}
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "", nil
	}

	ls := NewLogStreamer(opener, fetcher, sender)
	// Should not panic when stopping a non-existent stream
	ls.StopStream("nonexistent_container_id")
}

func TestStopAll(t *testing.T) {
	sender := &mockSender{}

	// Opener that blocks until context is cancelled
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		<-ctx.Done()
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "", nil
	}

	ls := NewLogStreamer(opener, fetcher, sender)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start a few streams that will block
	ls.StartStream(ctx, "container_aaaaaaa", "proj", "app", 10)
	ls.StartStream(ctx, "container_bbbbbbb", "proj", "postgres", 10)

	time.Sleep(50 * time.Millisecond)
	if ls.ActiveStreams() < 2 {
		t.Errorf("expected at least 2 active streams, got %d", ls.ActiveStreams())
	}

	ls.StopAll()

	time.Sleep(50 * time.Millisecond)
	if ls.ActiveStreams() != 0 {
		t.Errorf("expected 0 active streams after StopAll, got %d", ls.ActiveStreams())
	}
}

func TestStartStream_ReplacesExisting(t *testing.T) {
	sender := &mockSender{}

	callCount := 0
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		callCount++
		// Block until context cancelled
		<-ctx.Done()
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "", nil
	}

	ls := NewLogStreamer(opener, fetcher, sender)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first stream
	ls.StartStream(ctx, "container_xxxxxxxxx", "proj", "app", 10)
	time.Sleep(20 * time.Millisecond)

	// Start again for same container — should replace
	ls.StartStream(ctx, "container_xxxxxxxxx", "proj", "app", 10)
	time.Sleep(20 * time.Millisecond)

	if ls.ActiveStreams() != 1 {
		t.Errorf("expected 1 active stream (replacement), got %d", ls.ActiveStreams())
	}
}

func TestStreamLoop_SendsHistoricalAndLive(t *testing.T) {
	sender := &mockSender{}

	// History fetcher returns 2 lines
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "history line 1\nhistory line 2\n", nil
	}

	// Live opener sends one frame then blocks
	liveData := bytes.NewReader(
		append(
			buildTestFrame(streamStdout, "live line 1\n"),
			buildTestFrame(streamStderr, "live error\n")...,
		),
	)
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		return io.NopCloser(liveData), nil
	}

	ls := NewLogStreamer(opener, fetcher, sender)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ls.StartStream(ctx, "container_zzzzzzzz", "myapp", "app", 100)

	// Wait for stream to finish reading the data
	time.Sleep(200 * time.Millisecond)

	msgs := sender.getMessages()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages (history + live), got %d", len(msgs))
	}

	// First message should be log_history
	history, ok := msgs[0].(LogHistory)
	if !ok {
		// Might be sent as raw JSON via json.RawMessage
		t.Logf("first message type: %T", msgs[0])
	} else {
		if history.Type != "log_history" {
			t.Errorf("expected type 'log_history', got %q", history.Type)
		}
		if len(history.Lines) != 2 {
			t.Errorf("expected 2 history lines, got %d", len(history.Lines))
		}
	}

	// Live lines arrive as a batched log_lines message (Layer 4C 3C).
	if !containsLiveLine(msgs, "live line 1") || !containsLiveLine(msgs, "live error") {
		t.Error("live lines not found in batched log_lines message")
	}
}

// containsLiveLine reports whether any batched log_lines message carries line.
func containsLiveLine(msgs []interface{}, line string) bool {
	for _, msg := range msgs {
		if ll, ok := msg.(LogLines); ok && ll.Type == "log_lines" {
			for _, l := range ll.Lines {
				if l.Line == line {
					return true
				}
			}
		}
	}
	return false
}

func TestStreamLoop_FetchHistoryError_ContinuesLive(t *testing.T) {
	sender := &mockSender{}

	// History fetcher fails
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "", io.ErrUnexpectedEOF
	}

	// Live opener has data
	liveData := bytes.NewReader(buildTestFrame(streamStdout, "live data\n"))
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		return io.NopCloser(liveData), nil
	}

	ls := NewLogStreamer(opener, fetcher, sender)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ls.StartStream(ctx, "container_history_err", "myapp", "app", 100)
	time.Sleep(200 * time.Millisecond)

	// Should still get live log lines despite history failure
	msgs := sender.getMessages()
	if !containsLiveLine(msgs, "live data") {
		t.Error("live log line not found after history fetch failure")
	}
}

func blockingStreamer(max int) *LogStreamer {
	sender := &mockSender{}
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		<-ctx.Done()
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "", nil
	}
	ls := NewLogStreamer(opener, fetcher, sender)
	if max > 0 {
		ls.maxStreams = max
	}
	return ls
}

func TestStartStream_LimitReached(t *testing.T) {
	ls := blockingStreamer(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ls.StartStream(ctx, "container_1", "p1", "app", 10); err != nil {
		t.Fatalf("start 1: %v", err)
	}
	if err := ls.StartStream(ctx, "container_2", "p2", "app", 10); err != nil {
		t.Fatalf("start 2: %v", err)
	}
	err := ls.StartStream(ctx, "container_3", "p3", "app", 10)
	if err == nil {
		t.Fatal("expected limit error for third stream")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("expected limit error, got: %v", err)
	}
	if ls.ActiveStreams() != 2 {
		t.Errorf("expected 2 active streams, got %d", ls.ActiveStreams())
	}
}

func TestStartStream_ReplacementFreesSlot(t *testing.T) {
	// Replacing the same container's stream must not count against the limit.
	ls := blockingStreamer(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ls.StartStream(ctx, "container_x", "proj", "app", 10); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := ls.StartStream(ctx, "container_x", "proj", "app", 10); err != nil {
		t.Fatalf("replace at limit: %v", err)
	}
	if ls.ActiveStreams() != 1 {
		t.Errorf("expected 1 active stream after replacement, got %d", ls.ActiveStreams())
	}
}

func TestStreamLoop_SendsStreamEndedOnEOF(t *testing.T) {
	sender := &mockSender{}
	fetcher := func(ctx context.Context, id string, tail int) (string, error) {
		return "", nil
	}
	// One frame then EOF — simulates a container stopping.
	liveData := bytes.NewReader(buildTestFrame(streamStdout, "last line\n"))
	opener := func(ctx context.Context, id string) (io.ReadCloser, error) {
		return io.NopCloser(liveData), nil
	}

	ls := NewLogStreamer(opener, fetcher, sender)
	ls.batchFlushInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ls.StartStream(ctx, "container_eof", "myapp", "app", 100); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	for _, msg := range sender.getMessages() {
		if se, ok := msg.(LogStreamEnded); ok {
			if se.Type != "stream_ended" || se.Reason != "container_stopped" {
				t.Errorf("unexpected stream_ended: %+v", se)
			}
			if se.Container != "app" || se.Project != "myapp" {
				t.Errorf("stream_ended project/container wrong: %+v", se)
			}
			return
		}
	}
	t.Fatalf("stream_ended not sent; got %d messages", sender.count())
}

func TestHandleContainerReplaced_RedeploySwitchesStream(t *testing.T) {
	ls := blockingStreamer(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ls.StartStream(ctx, "container_old", "myshop", "app", 200); err != nil {
		t.Fatalf("start old: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	if err := ls.HandleContainerReplaced("myshop", "app", "container_new"); err != nil {
		t.Fatalf("handle replaced: %v", err)
	}

	// Handoff marker sent to the dashboard.
	markerSeen := false
	for _, msg := range ls.sender.(*mockSender).getMessages() {
		if ll, ok := msg.(LogLine); ok && ll.Line == "--- Redeploying myshop ---" {
			markerSeen = true
		}
	}
	if !markerSeen {
		t.Error("redeploy marker not sent")
	}

	// Stream switched to the new container.
	if ls.ActiveStreams() != 1 {
		t.Fatalf("expected 1 active stream, got %d", ls.ActiveStreams())
	}
	ls.mu.Lock()
	_, onNew := ls.activeStreams["container_new"]
	ls.mu.Unlock()
	if !onNew {
		t.Error("stream not switched to the new container")
	}
}

func TestHandleContainerReplaced_NoDesiredStream(t *testing.T) {
	ls := blockingStreamer(0)
	if err := ls.HandleContainerReplaced("nobody", "app", "container_x"); err != nil {
		t.Errorf("expected nil error when nobody is streaming, got %v", err)
	}
}

func TestStopProject(t *testing.T) {
	ls := blockingStreamer(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = ls.StartStream(ctx, "c1", "proj", "app", 10)
	_ = ls.StartStream(ctx, "c2", "proj", "postgres", 10)
	_ = ls.StartStream(ctx, "c3", "other", "app", 10)
	time.Sleep(20 * time.Millisecond)

	ls.StopProject("proj")

	if ls.ActiveStreams() != 1 {
		t.Errorf("expected 1 remaining stream, got %d", ls.ActiveStreams())
	}
	ls.mu.Lock()
	_, ok := ls.activeStreams["c3"]
	ls.mu.Unlock()
	if !ok {
		t.Error("other project's stream should remain")
	}
}

func TestLineBatcher_FlushesOnMaxLines(t *testing.T) {
	sender := &mockSender{}
	b := newLineBatcher(sender, "proj", "app", 3, time.Hour) // ticker effectively never fires
	defer b.Close()

	for i := 0; i < 3; i++ {
		b.Add(LogLine{Type: "log_line", Line: fmt.Sprintf("line %d", i)})
	}
	time.Sleep(20 * time.Millisecond)

	msgs := sender.getMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 log_lines batch, got %d", len(msgs))
	}
	batch, ok := msgs[0].(LogLines)
	if !ok || batch.Type != "log_lines" {
		t.Fatalf("expected LogLines message, got %T", msgs[0])
	}
	if len(batch.Lines) != 3 {
		t.Errorf("expected 3 lines in batch, got %d", len(batch.Lines))
	}
}

func TestLineBatcher_FlushesOnInterval(t *testing.T) {
	sender := &mockSender{}
	b := newLineBatcher(sender, "proj", "app", 100, 20*time.Millisecond)
	defer b.Close()

	b.Add(LogLine{Type: "log_line", Line: "single"})
	time.Sleep(80 * time.Millisecond)

	msgs := sender.getMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 timed batch, got %d", len(msgs))
	}
	batch, ok := msgs[0].(LogLines)
	if !ok || len(batch.Lines) != 1 {
		t.Fatalf("expected single-line batch, got %T", msgs[0])
	}
}

func TestParseDockerTimestamp(t *testing.T) {
	ts, text := parseDockerTimestamp("2024-01-15T10:00:30.123456789Z GET /api 200")
	if ts != "2024-01-15T10:00:30.123456789Z" {
		t.Errorf("ts = %q", ts)
	}
	if text != "GET /api 200" {
		t.Errorf("text = %q", text)
	}

	// No timestamp prefix — unchanged.
	ts, text = parseDockerTimestamp("plain log line")
	if ts != "" || text != "plain log line" {
		t.Errorf("plain: ts=%q text=%q", ts, text)
	}
}
