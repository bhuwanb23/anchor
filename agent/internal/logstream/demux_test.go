package logstream

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// buildFrame constructs a Docker log stream frame with the given stream type and payload.
func buildFrame(streamType int, payload []byte) []byte {
	header := make([]byte, headerSize)
	header[0] = byte(streamType)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	return append(header, payload...)
}

func TestDemuxReader_SingleFrame_Stdout(t *testing.T) {
	data := buildFrame(streamStdout, []byte("hello world\n"))
	demux := NewDemuxReader(bytes.NewReader(data))

	frame, err := demux.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.Stream != streamStdout {
		t.Errorf("expected stream %d, got %d", streamStdout, frame.Stream)
	}
	if string(frame.Data) != "hello world\n" {
		t.Errorf("expected 'hello world\\n', got %q", string(frame.Data))
	}
}

func TestDemuxReader_SingleFrame_Stderr(t *testing.T) {
	data := buildFrame(streamStderr, []byte("error message\n"))
	demux := NewDemuxReader(bytes.NewReader(data))

	frame, err := demux.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.Stream != streamStderr {
		t.Errorf("expected stream %d, got %d", streamStderr, frame.Stream)
	}
	if string(frame.Data) != "error message\n" {
		t.Errorf("expected 'error message\\n', got %q", string(frame.Data))
	}
}

func TestDemuxReader_MultipleFrames(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildFrame(streamStdout, []byte("line 1\n")))
	buf.Write(buildFrame(streamStderr, []byte("line 2\n")))
	buf.Write(buildFrame(streamStdout, []byte("line 3\n")))

	demux := NewDemuxReader(&buf)

	expected := []struct {
		stream int
		data   string
	}{
		{streamStdout, "line 1\n"},
		{streamStderr, "line 2\n"},
		{streamStdout, "line 3\n"},
	}

	for i, exp := range expected {
		frame, err := demux.ReadFrame()
		if err != nil {
			t.Fatalf("frame %d: unexpected error: %v", i, err)
		}
		if frame.Stream != exp.stream {
			t.Errorf("frame %d: expected stream %d, got %d", i, exp.stream, frame.Stream)
		}
		if string(frame.Data) != exp.data {
			t.Errorf("frame %d: expected %q, got %q", i, exp.data, string(frame.Data))
		}
	}
}

func TestDemuxReader_EmptyPayload(t *testing.T) {
	data := buildFrame(streamStdout, []byte{})
	demux := NewDemuxReader(bytes.NewReader(data))

	frame, err := demux.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frame.Data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(frame.Data))
	}
}

func TestDemuxReader_EOF(t *testing.T) {
	demux := NewDemuxReader(bytes.NewReader(nil))

	_, err := demux.ReadFrame()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestDemuxReader_TruncatedHeader(t *testing.T) {
	demux := NewDemuxReader(bytes.NewReader([]byte{0x01, 0x00})) // only 2 bytes

	_, err := demux.ReadFrame()
	if err == nil {
		t.Error("expected error for truncated header")
	}
}

func TestDemuxReader_TruncatedPayload(t *testing.T) {
	// Header says 10 bytes, but only 3 follow
	header := make([]byte, headerSize)
	header[0] = byte(streamStdout)
	binary.BigEndian.PutUint32(header[4:8], 10)
	data := append(header, []byte("abc")...)

	demux := NewDemuxReader(bytes.NewReader(data))

	_, err := demux.ReadFrame()
	if err == nil {
		t.Error("expected error for truncated payload")
	}
}

func TestLogFrame_StreamName(t *testing.T) {
	f1 := LogFrame{Stream: streamStdout}
	if f1.StreamName() != "stdout" {
		t.Errorf("expected 'stdout', got %q", f1.StreamName())
	}

	f2 := LogFrame{Stream: streamStderr}
	if f2.StreamName() != "stderr" {
		t.Errorf("expected 'stderr', got %q", f2.StreamName())
	}
}

func TestDemuxReader_LargePayload(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 64*1024) // 64KB
	data := buildFrame(streamStdout, payload)
	demux := NewDemuxReader(bytes.NewReader(data))

	frame, err := demux.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frame.Data) != 64*1024 {
		t.Errorf("expected 64KB, got %d bytes", len(frame.Data))
	}
}
