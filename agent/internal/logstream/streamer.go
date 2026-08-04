package logstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Stream limits and batching defaults (Layer 4C plan Step 3).
const (
	// maxStreams caps concurrent active log streams per agent.
	maxStreams = 10
	// defaultTail is the number of historical lines sent before live streaming.
	defaultTail = 200
	// batchMaxLines is the max lines buffered before a log_lines flush.
	batchMaxLines = 50
	// batchFlushInterval is the max time a line waits before a log_lines flush.
	batchFlushInterval = 100 * time.Millisecond
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
	id          uint64
	cancel      context.CancelFunc
	project     string
	role        string
	containerID string
	tail        int
}

// desiredStream records that a dashboard wants to watch a project+role,
// surviving a container stop so the stream can auto-resume on redeploy.
type desiredStream struct {
	project     string
	role        string
	containerID string
	tail        int
}

// LogStreamer manages active log streams for containers.
type LogStreamer struct {
	mu            sync.Mutex
	activeStreams map[string]streamEntry   // key: containerID
	desired       map[string]desiredStream // key: project+"/"+role
	batchers      map[uint64]*lineBatcher  // key: stream id (for memory remediation flush)
	nextID        atomic.Uint64
	openLogs      DockerLogOpener
	fetchHistory  DockerHistoricalLogsFunc
	sender        WSSender

	// Test seams (defaults set in NewLogStreamer).
	maxStreams         int
	batchMaxLines      int
	batchFlushInterval time.Duration
}

// NewLogStreamer creates a new log stream manager.
func NewLogStreamer(openLogs DockerLogOpener, fetchHistory DockerHistoricalLogsFunc, sender WSSender) *LogStreamer {
	return &LogStreamer{
		activeStreams:      make(map[string]streamEntry),
		desired:            make(map[string]desiredStream),
		batchers:           make(map[uint64]*lineBatcher),
		openLogs:           openLogs,
		fetchHistory:       fetchHistory,
		sender:             sender,
		maxStreams:         maxStreams,
		batchMaxLines:      batchMaxLines,
		batchFlushInterval: batchFlushInterval,
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

// LogLines is a batched set of live log lines (Layer 4C 3C). High-volume
// streams are flushed as log_lines batches (50 lines or 100ms, whichever first).
type LogLines struct {
	Type      string    `json:"type"`      // "log_lines"
	Project   string    `json:"project"`   // project name
	Container string    `json:"container"` // container role
	Lines     []LogLine `json:"lines"`     // live log lines
}

// LogHistory is the JSON structure for a batch of historical log lines.
type LogHistory struct {
	Type      string    `json:"type"`      // "log_history"
	Project   string    `json:"project"`   // project name
	Container string    `json:"container"` // container role
	Lines     []LogLine `json:"lines"`     // historical lines
}

// LogStreamEnded is sent when a stream ends because the container stopped
// (or the log source failed) — the dashboard shows "[Container stopped]".
type LogStreamEnded struct {
	Type      string `json:"type"`      // "stream_ended"
	Project   string `json:"project"`   // project name
	Container string `json:"container"` // container role
	Reason    string `json:"reason"`    // "container_stopped" or "read_error"
}

// projectKey builds the map key for a desired stream.
func projectKey(project, role string) string { return project + "/" + role }

// StartStream opens a Docker log stream for a container and forwards
// log lines to the control plane. It first sends historical lines (tail),
// then begins live streaming. Returns an error if the stream limit is hit.
//
// If a stream is already active for this container, it is replaced. A project
// may only have one stream per role; an existing stream for the same
// project+role is stopped first.
func (ls *LogStreamer) StartStream(ctx context.Context, containerID, projectName, containerRole string, tail int) error {
	if tail <= 0 {
		tail = defaultTail
	}

	ls.mu.Lock()
	// Count the slots this stream will occupy after replacements: the
	// container's own stream (if any) and any other stream of the same
	// project+role are replaced, not added.
	occupied := len(ls.activeStreams)
	if _, ok := ls.activeStreams[containerID]; ok {
		occupied--
	}
	for id, entry := range ls.activeStreams {
		if id != containerID && entry.project == projectName && entry.role == containerRole {
			occupied--
		}
	}
	if occupied >= ls.maxStreams {
		ls.mu.Unlock()
		return fmt.Errorf("log stream limit reached (%d active); stop another stream first", ls.maxStreams)
	}
	// Replace an existing stream for the same container.
	if entry, ok := ls.activeStreams[containerID]; ok {
		entry.cancel()
		delete(ls.activeStreams, containerID)
	}
	// One stream per project+role: stop any other container streaming it.
	for id, entry := range ls.activeStreams {
		if entry.project == projectName && entry.role == containerRole && id != containerID {
			entry.cancel()
			delete(ls.activeStreams, id)
		}
	}

	streamID := ls.nextID.Add(1)
	streamCtx, cancel := context.WithCancel(ctx)

	ls.activeStreams[containerID] = streamEntry{
		id:          streamID,
		cancel:      cancel,
		project:     projectName,
		role:        containerRole,
		containerID: containerID,
		tail:        tail,
	}
	// Remember this stream is wanted so it can auto-resume after a redeploy.
	ls.desired[projectKey(projectName, containerRole)] = desiredStream{
		project:     projectName,
		role:        containerRole,
		containerID: containerID,
		tail:        tail,
	}
	ls.mu.Unlock()

	slog.Info("log stream started",
		"container", shortID(containerID), "project", projectName, "role", containerRole)
	go ls.streamLoop(streamCtx, containerID, streamID, projectName, containerRole, tail)
	return nil
}

// StopStream stops a live log stream for the given container ID.
func (ls *LogStreamer) StopStream(containerID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if entry, ok := ls.activeStreams[containerID]; ok {
		entry.cancel()
		delete(ls.activeStreams, containerID)
		delete(ls.desired, projectKey(entry.project, entry.role))
		slog.Info("stopped log stream", "container", shortID(containerID))
	}
}

// StopProject stops all active streams for a project and forgets their
// desired entries (e.g. when a project is deleted).
func (ls *LogStreamer) StopProject(project string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	for id, entry := range ls.activeStreams {
		if entry.project == project {
			entry.cancel()
			delete(ls.activeStreams, id)
			delete(ls.desired, projectKey(project, entry.role))
		}
	}
}

// StopAll stops all active log streams.
func (ls *LogStreamer) StopAll() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	count := len(ls.activeStreams)
	for id, entry := range ls.activeStreams {
		entry.cancel()
		delete(ls.activeStreams, id)
	}
	ls.desired = make(map[string]desiredStream)
	if count > 0 {
		slog.Info("stopped all log streams", "count", count)
	}
}

// ActiveStreams returns the number of currently active streams.
func (ls *LogStreamer) ActiveStreams() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.activeStreams)
}

// FlushAllBuffers immediately flushes every active stream's line batcher and
// returns how many buffers were flushed. Used by Layer 4C Step 7
// auto-remediation when the agent's own memory usage is high (log buffers are
// the largest soft memory consumers).
func (ls *LogStreamer) FlushAllBuffers() int {
	ls.mu.Lock()
	batchers := make([]*lineBatcher, 0, len(ls.batchers))
	for _, b := range ls.batchers {
		batchers = append(batchers, b)
	}
	ls.mu.Unlock()
	for _, b := range batchers {
		b.Flush()
	}
	return len(batchers)
}

// HandleContainerReplaced is called by Layer 4B after a redeploy starts a new
// container. If a dashboard is watching this project+role, the stream is
// automatically switched to the new container with a handoff marker.
func (ls *LogStreamer) HandleContainerReplaced(projectName, role, newContainerID string) error {
	key := projectKey(projectName, role)

	ls.mu.Lock()
	desired, ok := ls.desired[key]
	ls.mu.Unlock()
	if !ok {
		return nil // nobody is streaming this project+role
	}

	if desired.containerID != newContainerID {
		// Redeploy: the old container was replaced. Send a handoff marker so
		// the dashboard shows the transition, then switch to the new container.
		marker := LogLine{
			Type:      "log_line",
			Project:   projectName,
			Container: role,
			Stream:    "stdout",
			Line:      fmt.Sprintf("--- Redeploying %s ---", projectName),
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := ls.sender.SendJSON(marker); err != nil {
			slog.Warn("failed to send redeploy marker", "error", err)
		}
		ls.StopStream(desired.containerID)
	}

	// Same container (docker restart) or a brand-new one: (re)start the live
	// stream. StartStream replaces any still-active stream for this container.
	return ls.StartStream(context.Background(), newContainerID, projectName, role, desired.tail)
}

func (ls *LogStreamer) streamLoop(ctx context.Context, containerID string, streamID uint64, projectName, containerRole string, tail int) {
	naturalEnd := false

	defer func() {
		ls.mu.Lock()
		// Only remove if this is still the active stream for this container.
		// If a replacement stream was started, its ID will be higher.
		if entry, ok := ls.activeStreams[containerID]; ok && entry.id == streamID {
			delete(ls.activeStreams, containerID)
		}
		ls.mu.Unlock()

		// A stream that ended because the container stopped keeps its desired
		// entry so a redeploy can auto-resume it.
		if naturalEnd {
			slog.Info("log stream ended (container stopped)",
				"container", shortID(containerID))
		}
	}()

	// Fetch historical logs first
	if ls.fetchHistory != nil {
		if err := ls.sendHistoricalLogs(ctx, containerID, projectName, containerRole, tail); err != nil {
			slog.Warn("failed to fetch historical logs",
				"container", shortID(containerID), "error", err)
		}
	}

	// Open live stream
	reader, err := ls.openLogs(ctx, containerID)
	if err != nil {
		slog.Error("failed to open log stream",
			"container", shortID(containerID), "error", err)
		return
	}
	defer reader.Close()

	batcher := newLineBatcher(ls.sender, projectName, containerRole, ls.batchMaxLines, ls.batchFlushInterval)
	ls.mu.Lock()
	ls.batchers[streamID] = batcher
	ls.mu.Unlock()
	defer func() {
		ls.mu.Lock()
		delete(ls.batchers, streamID)
		ls.mu.Unlock()
	}()
	defer batcher.Close()

	slog.Info("live log stream started",
		"container", shortID(containerID), "project", projectName, "role", containerRole)

	demux := NewDemuxReader(reader)
	for {
		select {
		case <-ctx.Done():
			slog.Info("log stream context cancelled", "container", shortID(containerID))
			return
		default:
		}

		frame, err := demux.ReadFrame()
		if err != nil {
			if err == io.EOF {
				if ctx.Err() == nil {
					naturalEnd = true
				}
			} else if ctx.Err() == nil {
				slog.Warn("log stream read error",
					"container", shortID(containerID), "error", err)
				naturalEnd = true
			}
			if naturalEnd {
				reason := "container_stopped"
				if err != io.EOF {
					reason = "read_error"
				}
				// Flush any buffered lines first so the dashboard sees the final
				// output before the "[Container stopped]" marker.
				batcher.Flush()
				_ = ls.sender.SendJSON(LogStreamEnded{
					Type:      "stream_ended",
					Project:   projectName,
					Container: containerRole,
					Reason:    reason,
				})
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
			// Docker prefixes each line with an RFC3339Nano timestamp when
			// Timestamps=true; prefer it over the receive time.
			ts, text := parseDockerTimestamp(line)
			if ts == "" {
				ts = time.Now().UTC().Format(time.RFC3339Nano)
			}
			msg := LogLine{
				Type:      "log_line",
				Project:   projectName,
				Container: containerRole,
				Stream:    frame.StreamName(),
				Line:      text,
				Timestamp: ts,
			}
			batcher.Add(msg)
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
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
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

// lineBatcher buffers live log lines and flushes them as a single log_lines
// message when the buffer fills (maxLines) or the flush interval elapses —
// whichever comes first (Layer 4C plan Step 3C).
type lineBatcher struct {
	mu        sync.Mutex
	lines     []LogLine
	maxLines  int
	interval  time.Duration
	sender    WSSender
	project   string
	container string
	stop      chan struct{}
	closed    bool
}

func newLineBatcher(sender WSSender, project, container string, maxLines int, interval time.Duration) *lineBatcher {
	b := &lineBatcher{
		maxLines:  maxLines,
		interval:  interval,
		sender:    sender,
		project:   project,
		container: container,
		stop:      make(chan struct{}),
	}
	go b.runTicker()
	return b
}

func (b *lineBatcher) runTicker() {
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.Flush()
		}
	}
}

// Add appends a line and flushes immediately if the buffer is full.
func (b *lineBatcher) Add(line LogLine) {
	b.mu.Lock()
	b.lines = append(b.lines, line)
	full := len(b.lines) >= b.maxLines
	b.mu.Unlock()
	if full {
		b.Flush()
	}
}

// Flush sends any buffered lines as a log_lines message.
func (b *lineBatcher) Flush() {
	b.mu.Lock()
	if len(b.lines) == 0 {
		b.mu.Unlock()
		return
	}
	lines := b.lines
	b.lines = nil
	b.mu.Unlock()

	msg := LogLines{
		Type:      "log_lines",
		Project:   b.project,
		Container: b.container,
		Lines:     lines,
	}
	if err := b.sender.SendJSON(msg); err != nil {
		slog.Warn("failed to send log_lines batch",
			"container", b.container, "lines", len(lines), "error", err)
	}
}

// Close stops the ticker and flushes any remaining lines.
func (b *lineBatcher) Close() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.stop)
	}
	b.mu.Unlock()
	b.Flush()
}

// parseDockerTimestamp extracts Docker's RFC3339Nano timestamp prefix
// ("2024-01-15T10:00:30.123456789Z content") when present. Returns the
// formatted timestamp and the remaining text, or ("", line) when the line
// has no timestamp prefix.
func parseDockerTimestamp(line string) (ts, text string) {
	idx := strings.Index(line, " ")
	if idx <= 0 {
		return "", line
	}
	t, err := time.Parse(time.RFC3339Nano, line[:idx])
	if err != nil {
		return "", line
	}
	return t.UTC().Format(time.RFC3339Nano), line[idx+1:]
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
