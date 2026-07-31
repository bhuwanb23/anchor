package logstream

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WSSender is the interface for sending JSON messages over WebSocket.
// Implemented by ws.Client.SendJSON.
type WSSender interface {
	SendJSON(v interface{}) error
}

// DockerLogOpener opens a Docker log stream (follow=true) for a given container ID.
type DockerLogOpener func(ctx context.Context, containerID string) (io.ReadCloser, error)

// DockerHistoricalLogsFunc fetches historical (non-follow) log lines for a container.
type DockerHistoricalLogsFunc func(ctx context.Context, containerID string, tail int) (string, error)

// streamEntry tracks an active stream with a unique ID so cleanup
// doesn't accidentally remove a replacement stream.
type streamEntry struct {
	id     uint64
	cancel context.CancelFunc
}

// LogStreamer manages active log streams for containers.
type LogStreamer struct {
	mu            sync.Mutex
	activeStreams map[string]streamEntry
	nextID        atomic.Uint64
	openLogs      DockerLogOpener
	fetchHistory  DockerHistoricalLogsFunc
	sender        WSSender
}

// NewLogStreamer creates a new log stream manager.
func NewLogStreamer(openLogs DockerLogOpener, fetchHistory DockerHistoricalLogsFunc, sender WSSender) *LogStreamer {
	return &LogStreamer{
		activeStreams: make(map[string]streamEntry),
		openLogs:      openLogs,
		fetchHistory:  fetchHistory,
		sender:        sender,
	}
}

// StreamLogsPayload is the payload for a stream_logs command.
type StreamLogsPayload struct {
	ProjectName string   `json:"project_name"`
	Containers  []string `json:"containers"` // container roles: "app", "postgres", "redis"
	Tail        int      `json:"tail"`       // historical lines to fetch first (default 200)
}

// LogLine is the JSON structure sent to the control plane for each log line.
type LogLine struct {
	Type      string `json:"type"`      // "log_line"
	Project   string `json:"project"`   // project name
	Container string `json:"container"` // container role (app, postgres, redis)
	Stream    string `json:"stream"`    // "stdout" or "stderr"
	Line      string `json:"line"`      // the log line content
	Timestamp string `json:"timestamp"` // RFC3339 timestamp
}

// LogHistory is the JSON structure for a batch of historical log lines.
type LogHistory struct {
	Type      string    `json:"type"`      // "log_history"
	Project   string    `json:"project"`   // project name
	Container string    `json:"container"` // container role
	Lines     []LogLine `json:"lines"`     // historical lines
}

// StartStream opens a Docker log stream for a container and forwards
// log lines to the control plane. It first sends historical lines (tail),
// then begins live streaming.
//
// If a stream is already active for this containerID, it is stopped first.
func (ls *LogStreamer) StartStream(ctx context.Context, containerID, projectName, containerRole string, tail int) {
	ls.StopStream(containerID)

	streamID := ls.nextID.Add(1)
	streamCtx, cancel := context.WithCancel(ctx)

	ls.mu.Lock()
	ls.activeStreams[containerID] = streamEntry{id: streamID, cancel: cancel}
	ls.mu.Unlock()

	go ls.streamLoop(streamCtx, containerID, streamID, projectName, containerRole, tail)
}

// StopStream stops a live log stream for the given container ID.
func (ls *LogStreamer) StopStream(containerID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if entry, ok := ls.activeStreams[containerID]; ok {
		entry.cancel()
		delete(ls.activeStreams, containerID)
		slog.Info("stopped log stream", "container", containerID[:12])
	}
}

// StopAll stops all active log streams.
func (ls *LogStreamer) StopAll() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	for id, entry := range ls.activeStreams {
		entry.cancel()
		delete(ls.activeStreams, id)
	}
	slog.Info("stopped all log streams", "count", len(ls.activeStreams))
}

// ActiveStreams returns the number of currently active streams.
func (ls *LogStreamer) ActiveStreams() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.activeStreams)
}

func (ls *LogStreamer) streamLoop(ctx context.Context, containerID string, streamID uint64, projectName, containerRole string, tail int) {
	defer func() {
		ls.mu.Lock()
		// Only remove if this is still the active stream for this container.
		// If a replacement stream was started, its ID will be higher.
		if entry, ok := ls.activeStreams[containerID]; ok && entry.id == streamID {
			delete(ls.activeStreams, containerID)
		}
		ls.mu.Unlock()
	}()

	if tail <= 0 {
		tail = 200
	}

	// Fetch historical logs first
	if ls.fetchHistory != nil {
		if err := ls.sendHistoricalLogs(ctx, containerID, projectName, containerRole, tail); err != nil {
			slog.Warn("failed to fetch historical logs",
				"container", containerID[:12],
				"error", err)
		}
	}

	// Open live stream
	reader, err := ls.openLogs(ctx, containerID)
	if err != nil {
		slog.Error("failed to open log stream",
			"container", containerID[:12],
			"error", err)
		return
	}
	defer reader.Close()

	slog.Info("live log stream started",
		"container", containerID[:12],
		"project", projectName,
		"role", containerRole)

	demux := NewDemuxReader(reader)
	for {
		select {
		case <-ctx.Done():
			slog.Info("log stream context cancelled",
				"container", containerID[:12])
			return
		default:
		}

		frame, err := demux.ReadFrame()
		if err != nil {
			if err == io.EOF {
				slog.Info("log stream ended (container stopped)",
					"container", containerID[:12])
			} else if ctx.Err() != nil {
				// Context cancelled, normal shutdown
			} else {
				slog.Warn("log stream read error",
					"container", containerID[:12],
					"error", err)
			}
			return
		}

		if len(frame.Data) == 0 {
			continue
		}

		// Docker may send multiple lines in a single frame
		lines := strings.Split(strings.TrimRight(string(frame.Data), "\n"), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			msg := LogLine{
				Type:      "log_line",
				Project:   projectName,
				Container: containerRole,
				Stream:    frame.StreamName(),
				Line:      line,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			if err := ls.sender.SendJSON(msg); err != nil {
				slog.Warn("failed to send log line",
					"container", containerID[:12],
					"error", err)
			}
		}
	}
}

func (ls *LogStreamer) sendHistoricalLogs(ctx context.Context, containerID, projectName, containerRole string, tail int) error {
	raw, err := ls.fetchHistory(ctx, containerID, tail)
	if err != nil {
		return err
	}

	if raw == "" {
		return nil
	}

	var lines []LogLine
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if line == "" {
			continue
		}
		lines = append(lines, LogLine{
			Type:      "log_line",
			Project:   projectName,
			Container: containerRole,
			Stream:    "stdout",
			Line:      line,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}

	if len(lines) == 0 {
		return nil
	}

	history := LogHistory{
		Type:      "log_history",
		Project:   projectName,
		Container: containerRole,
		Lines:     lines,
	}

	data, err := json.Marshal(history)
	if err != nil {
		return err
	}

	return ls.sender.SendJSON(json.RawMessage(data))
}
