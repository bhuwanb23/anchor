package lifecycle

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yourname/yourplatform/agent/internal/executor"
	"github.com/yourname/yourplatform/agent/internal/preflight"
	"github.com/yourname/yourplatform/agent/internal/state"
	"github.com/yourname/yourplatform/agent/internal/ws"
)

// Manager owns the agent↔control-plane WebSocket lifecycle and command dispatch.
type Manager struct {
	client       *ws.Client
	exec         *executor.Executor
	stateMgr     *state.Manager
	dataDir      string
	serverID     string
	preflight    *preflight.Result
	accepting    atomic.Bool
	inFlight     sync.WaitGroup
	onUpdatePush func(version string)
}

// NewManager creates a connection manager.
func NewManager(client *ws.Client, exec *executor.Executor, stateMgr *state.Manager, dataDir, serverID string) *Manager {
	m := &Manager{
		client:   client,
		exec:     exec,
		stateMgr: stateMgr,
		dataDir:  dataDir,
		serverID: serverID,
	}
	m.accepting.Store(true)
	if exec != nil {
		exec.WithProgressSender(client).WithServerID(serverID)
	}
	return m
}

// SetPreflightResult stores preflight output to send after connect.
func (m *Manager) SetPreflightResult(r *preflight.Result) {
	m.preflight = r
}

// OnUpdateAvailable registers a handler for update_available pushes.
func (m *Manager) OnUpdateAvailable(fn func(version string)) {
	m.onUpdatePush = fn
}

// StopAccepting rejects new commands (graceful shutdown).
func (m *Manager) StopAccepting() {
	m.accepting.Store(false)
}

// WaitInFlight waits for in-flight commands up to timeout.
func (m *Manager) WaitInFlight(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		m.inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("timed out waiting for in-flight commands")
	}
}

// NotifyShutdown tells the control plane the agent is shutting down cleanly.
func (m *Manager) NotifyShutdown() {
	_ = m.client.SendJSON(map[string]interface{}{
		"type": "agent_shutdown",
		"payload": map[string]interface{}{
			"server_id": m.serverID,
			"reason":    "sigterm",
		},
	})
}

// Run starts reconnect loop and message dispatch. Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	go m.dispatchLoop(ctx)

	onConnect := func() {
		if err := MarkConnected(m.dataDir); err != nil {
			slog.Warn("failed to write agent.connected", "error", err)
		}
		if m.stateMgr != nil {
			_ = m.stateMgr.RecordConnected()
		}
		m.sendPreflight()
		m.sendHello()
	}
	onDisconnect := func() {
		ClearConnected(m.dataDir)
		if m.stateMgr != nil {
			_ = m.stateMgr.RecordDisconnected()
		}
	}

	m.client.SetConnectHooks(onConnect, onDisconnect)
	m.client.Run(ctx)
}

func (m *Manager) sendHello() {
	_ = m.client.SendJSON(map[string]interface{}{
		"type": "hello",
		"payload": map[string]string{
			"server_id": m.serverID,
		},
	})
}

func (m *Manager) sendPreflight() {
	if m.preflight == nil {
		return
	}
	payload, err := json.Marshal(m.preflight)
	if err != nil {
		slog.Warn("marshal preflight result", "error", err)
		return
	}
	msg := map[string]interface{}{
		"type":    "preflight_result",
		"payload": json.RawMessage(payload),
	}
	if err := m.client.SendJSON(msg); err != nil {
		slog.Warn("send preflight_result", "error", err)
	} else {
		slog.Info("sent preflight_result to control plane")
	}
}

func (m *Manager) dispatchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-m.client.Recv():
			if !ok {
				return
			}
			m.handleMessage(ctx, msg)
		}
	}
}

func (m *Manager) handleMessage(ctx context.Context, msg ws.Message) {
	switch msg.Type {
	case "register_ack":
		slog.Info("received register_ack", "server_id", msg.ServerID)
		if msg.ServerID != "" {
			m.serverID = msg.ServerID
			if m.exec != nil {
				m.exec.WithServerID(msg.ServerID)
			}
		}
		m.ingestPendingFromPayload(ctx, msg.Payload)

	case "hello_ack":
		m.ingestPendingFromPayload(ctx, msg.Payload)

	case "heartbeat":
		_ = m.client.SendJSON(map[string]string{"type": "pong"})

	case "update_available":
		var p struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(msg.Payload, &p)
		if m.onUpdatePush != nil && p.Version != "" {
			m.onUpdatePush(p.Version)
		}

	case "command":
		m.handleCommand(ctx, msg.Payload)

	case "deploy", "rollback", "restart", "stop", "backup", "fetch_logs":
		raw, _ := json.Marshal(msg)
		var flat struct {
			Type       string          `json:"type"`
			Deployment string          `json:"deployment"`
			Payload    json.RawMessage `json:"payload"`
		}
		_ = json.Unmarshal(raw, &flat)
		id := flat.Deployment
		if id == "" {
			id = "cmd-" + time.Now().Format("150405")
		}
		payload := flat.Payload
		if len(payload) == 0 {
			payload = msg.Payload
		}
		m.runCommand(ctx, executor.Command{ID: id, Type: flat.Type, Payload: payload})

	default:
		slog.Debug("ignored ws message", "type", msg.Type)
	}
}

func (m *Manager) ingestPendingFromPayload(ctx context.Context, payload json.RawMessage) {
	if len(payload) == 0 {
		return
	}
	var body struct {
		PendingCommands []executor.Command `json:"pending_commands"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		// register_ack may not have payload wrapper — try top-level from message already handled
		return
	}
	if len(body.PendingCommands) == 0 {
		return
	}
	slog.Info("processing pending commands from hello_ack", "count", len(body.PendingCommands))
	m.exec.ProcessPendingCommands(ctx, body.PendingCommands, m.sendResult)
}

func (m *Manager) handleCommand(ctx context.Context, payload json.RawMessage) {
	var cmd executor.Command
	if err := json.Unmarshal(payload, &cmd); err != nil {
		slog.Warn("invalid command payload", "error", err)
		return
	}
	if cmd.Type == "" {
		slog.Warn("command missing type")
		return
	}
	m.runCommand(ctx, cmd)
}

func (m *Manager) runCommand(ctx context.Context, cmd executor.Command) {
	if !m.accepting.Load() {
		m.sendResult(executor.Result{
			CommandID: cmd.ID,
			Status:    "failed",
			Error:     "agent is shutting down",
			Timestamp: time.Now().UTC(),
		})
		return
	}

	m.inFlight.Add(1)
	m.exec.Dispatch(ctx, cmd, func(result executor.Result) {
		defer m.inFlight.Done()
		m.sendResult(result)
	})
}

func (m *Manager) sendResult(result executor.Result) {
	_ = m.client.SendJSON(map[string]interface{}{
		"type": "result",
		"payload": ws.ResultPayload{
			CommandID: result.CommandID,
			Status:    result.Status,
			Output:    result.Output,
			Error:     result.Error,
		},
	})
}
