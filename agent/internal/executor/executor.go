package executor

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

type Command struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type Result struct {
	CommandID string          `json:"command_id"`
	Status    string          `json:"status"`
	Output    string          `json:"output"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

type CommandQueue struct {
	commands []Command
}

func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		commands: make([]Command, 0),
	}
}

func (q *CommandQueue) Enqueue(cmd Command) {
	slog.Info("enqueuing command", "id", cmd.ID, "type", cmd.Type)
	q.commands = append(q.commands, cmd)
}

func (q *CommandQueue) Dequeue() (Command, bool) {
	if len(q.commands) == 0 {
		return Command{}, false
	}

	cmd := q.commands[0]
	q.commands = q.commands[1:]
	return cmd, true
}

func (q *CommandQueue) Len() int {
	return len(q.commands)
}

func (q *CommandQueue) Clear() {
	q.commands = make([]Command, 0)
}

func Execute(cmd Command) Result {
	slog.Info("executing command", "id", cmd.ID, "type", cmd.Type)

	result := Result{
		CommandID: cmd.ID,
		Timestamp: time.Now().UTC(),
	}

	switch cmd.Type {
	case "deploy":
		result = executeDeploy(cmd)
	case "rollback":
		result = executeRollback(cmd)
	case "restart":
		result = executeRestart(cmd)
	case "stop":
		result = executeStop(cmd)
	case "backup":
		result = executeBackup(cmd)
	case "fetch_logs":
		result = executeFetchLogs(cmd)
	default:
		result.Status = "error"
		result.Error = fmt.Sprintf("unknown command type: %s", cmd.Type)
	}

	return result
}

func executeDeploy(cmd Command) Result {
	// TODO: implement deploy logic
	return Result{
		CommandID: cmd.ID,
		Status:    "queued",
		Output:    "deployment queued for execution",
	}
}

func executeRollback(cmd Command) Result {
	// TODO: implement rollback logic
	return Result{
		CommandID: cmd.ID,
		Status:    "not_implemented",
		Output:    "rollback not yet implemented",
	}
}

func executeRestart(cmd Command) Result {
	// TODO: implement restart logic
	return Result{
		CommandID: cmd.ID,
		Status:    "queued",
		Output:    "restart queued for execution",
	}
}

func executeStop(cmd Command) Result {
	// TODO: implement stop logic
	return Result{
		CommandID: cmd.ID,
		Status:    "queued",
		Output:    "stop queued for execution",
	}
}

func executeBackup(cmd Command) Result {
	// TODO: implement backup logic
	return Result{
		CommandID: cmd.ID,
		Status:    "queued",
		Output:    "backup queued for execution",
	}
}

func executeFetchLogs(cmd Command) Result {
	// TODO: implement log fetch logic
	return Result{
		CommandID: cmd.ID,
		Status:    "queued",
		Output:    "log fetch queued for execution",
	}
}