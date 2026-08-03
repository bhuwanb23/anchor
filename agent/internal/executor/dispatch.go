package executor

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// DispatchResult is returned after a command is accepted into the dispatcher.
type DispatchResult struct {
	Acked      bool
	Cached     bool
	Expired    bool
	Rejected   bool
	RejectReason string
}

// Dispatch validates, acks, and schedules command execution on the project slot.
// The result is sent via progressSender when execution finishes.
// Log-stream commands bypass slots and run immediately.
func (e *Executor) Dispatch(ctx context.Context, cmd Command, onComplete func(Result)) DispatchResult {
	dr := DispatchResult{}

	if cmd.ID == "" {
		cmd.ID = fmt.Sprintf("cmd-%d", time.Now().UnixNano())
	}

	// Envelope validation
	if cmd.ServerID != "" && e.serverID != "" && cmd.ServerID != e.serverID {
		dr.Rejected = true
		dr.RejectReason = fmt.Sprintf("server_id mismatch: got %s want %s", cmd.ServerID, e.serverID)
		result := Result{CommandID: cmd.ID, Status: "failed", Error: dr.RejectReason, Timestamp: time.Now().UTC()}
		if onComplete != nil {
			onComplete(result)
		}
		return dr
	}

	env := CommandEnvelope{
		ID: cmd.ID, Type: cmd.Type, ServerID: cmd.ServerID,
		IssuedBy: cmd.IssuedBy, IssuedAt: cmd.IssuedAt, ExpiresAt: cmd.ExpiresAt,
		Payload: cmd.Payload,
	}
	if env.IsExpired(time.Now()) {
		dr.Expired = true
		result := Result{CommandID: cmd.ID, Status: "expired", Error: "command expired while offline or before execution", Timestamp: time.Now().UTC()}
		e.idempotency.Put(cmd.ID, result)
		SendAck(e.progressSender, cmd.ID)
		dr.Acked = true
		if onComplete != nil {
			onComplete(result)
		}
		return dr
	}

	// Idempotency
	if cached, ok := e.idempotency.Get(cmd.ID); ok {
		dr.Cached = true
		SendAck(e.progressSender, cmd.ID)
		dr.Acked = true
		if onComplete != nil {
			onComplete(cached)
		}
		return dr
	}

	SendAck(e.progressSender, cmd.ID)
	dr.Acked = true

	// Log streams bypass project slots
	if cmd.Type == "start_log_stream" || cmd.Type == "stop_log_stream" {
		go e.runSafe(ctx, cmd, onComplete)
		return dr
	}

	projectKey := ProjectKeyFromCommand(cmd)
	e.slots.Submit(projectKey, func() {
		e.runSafe(ctx, cmd, onComplete)
	})
	return dr
}

func (e *Executor) runSafe(ctx context.Context, cmd Command, onComplete func(Result)) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("command handler panicked", "id", cmd.ID, "type", cmd.Type, "panic", r)
			result := Result{
				CommandID: cmd.ID,
				Status:    "failed",
				Error:     fmt.Sprintf("internal error: %v", r),
				Timestamp: time.Now().UTC(),
			}
			e.idempotency.Put(cmd.ID, result)
			if onComplete != nil {
				onComplete(result)
			}
		}
	}()

	result := e.Execute(ctx, cmd)
	if result.Status == "" {
		if result.Error != "" {
			result.Status = "failed"
		} else {
			result.Status = "success"
		}
	}
	// Normalize "error" → "failed" for CP consistency
	if result.Status == "error" {
		result.Status = "failed"
	}
	e.idempotency.Put(cmd.ID, result)
	if onComplete != nil {
		onComplete(result)
	}
}

// ProcessPendingCommands handles hello_ack pending_commands list.
// For deploy commands targeting the same project, only the newest is kept.
func (e *Executor) ProcessPendingCommands(ctx context.Context, commands []Command, onComplete func(Result)) {
	// Dedupe deploys: keep newest per project
	newestDeploy := map[string]int{} // project -> index
	skip := map[int]bool{}
	for i, cmd := range commands {
		if cmd.Type != "deploy" {
			continue
		}
		pk := ProjectKeyFromCommand(cmd)
		if prev, ok := newestDeploy[pk]; ok {
			// keep later one; skip earlier
			skip[prev] = true
		}
		newestDeploy[pk] = i
	}

	for i, cmd := range commands {
		if skip[i] {
			result := Result{
				CommandID: cmd.ID,
				Status:    "skipped",
				Error:     "superseded by newer deploy for same project",
				Timestamp: time.Now().UTC(),
			}
			if onComplete != nil {
				onComplete(result)
			}
			continue
		}
		e.Dispatch(ctx, cmd, onComplete)
	}
}
