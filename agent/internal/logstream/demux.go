package logstream

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	headerSize   = 8
	streamStdout = 1
	streamStderr = 2
)

// LogFrame represents a single frame from Docker's multiplexed log stream.
type LogFrame struct {
	Stream int    // 1 = stdout, 2 = stderr
	Data   []byte // payload (one or more log lines)
}

// StreamName returns a human-readable name for the stream type.
func (f LogFrame) StreamName() string {
	if f.Stream == streamStderr {
		return "stderr"
	}
	return "stdout"
}

// DemuxReader reads Docker's multiplexed log stream and yields individual frames.
// Docker prefix每一帧 with an 8-byte header:
//
//	[stream_type, 0, 0, 0, size_byte3, size_byte2, size_byte1, size_byte0]
//
// stream_type: 1=stdout, 2=stderr
// size: 4-byte big-endian payload length
type DemuxReader struct {
	reader io.Reader
	header [headerSize]byte
}

// NewDemuxReader wraps an io.Reader that produces Docker multiplexed output.
func NewDemuxReader(r io.Reader) *DemuxReader {
	return &DemuxReader{reader: r}
}

// ReadFrame reads the next log frame from the stream.
// Returns io.EOF when the stream ends (container stopped or follow cancelled).
func (d *DemuxReader) ReadFrame() (LogFrame, error) {
	_, err := io.ReadFull(d.reader, d.header[:])
	if err != nil {
		return LogFrame{}, err
	}

	streamType := int(d.header[0])
	size := binary.BigEndian.Uint32(d.header[4:8])

	if size == 0 {
		return LogFrame{Stream: streamType}, nil
	}

	payload := make([]byte, size)
	_, err = io.ReadFull(d.reader, payload)
	if err != nil {
		return LogFrame{}, fmt.Errorf("read payload: %w", err)
	}

	return LogFrame{Stream: streamType, Data: payload}, nil
}
