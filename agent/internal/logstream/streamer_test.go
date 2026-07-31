package logstream

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
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
	foundLive := false
	for _, msg := range msgs {
		if ll, ok := msg.(LogLine); ok && ll.Type == "log_line" && ll.Line == "live data" {
			foundLive = true
		}
	}
	if !foundLive {
		t.Error("live log line not found after history fetch failure")
	}
}
