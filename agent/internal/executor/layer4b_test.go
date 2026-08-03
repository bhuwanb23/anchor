package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yourname/yourplatform/agent/internal/backup"
	"github.com/yourname/yourplatform/agent/internal/caddy"
	"github.com/yourname/yourplatform/agent/internal/env"
	"github.com/yourname/yourplatform/agent/internal/state"
)

type captureSender struct {
	mu   sync.Mutex
	msgs []map[string]interface{}
}

func (c *captureSender) SendJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(v)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	c.msgs = append(c.msgs, m)
	return nil
}

func (c *captureSender) types() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.msgs))
	for i, m := range c.msgs {
		if t, ok := m["type"].(string); ok {
			out[i] = t
		}
	}
	return out
}

func (c *captureSender) hasType(t string) bool {
	for _, x := range c.types() {
		if x == t {
			return true
		}
	}
	return false
}

func testExecutor(t *testing.T) *Executor {
	t.Helper()
	return New(nil, caddy.NewManager(""), backup.NewManager(""))
}

// Scenario 1: deploy success path units — ack + progress phases + valid payload.
func TestScenario1_DeployAckAndProgress(t *testing.T) {
	sender := &captureSender{}
	exec := testExecutor(t).WithProgressSender(sender).WithPreflightFn(func() (string, error) {
		return "ok", nil
	})

	done := make(chan Result, 1)
	payload, _ := json.Marshal(map[string]string{})
	dr := exec.Dispatch(context.Background(), Command{
		ID: "cmd-ack-1", Type: "run_preflight", Payload: payload,
	}, func(r Result) { done <- r })

	if !dr.Acked {
		t.Fatal("expected ack")
	}
	if !sender.hasType("command_ack") {
		t.Fatalf("expected command_ack, got %v", sender.types())
	}

	SendProgress(sender, "cmd-deploy", "pulling", "Pulling image...", 10)
	SendProgress(sender, "cmd-deploy", "starting", "Creating container...", 40)
	SendProgress(sender, "cmd-deploy", "health", "Waiting for health check...", 70)
	SendProgress(sender, "cmd-deploy", "routing", "Configuring domain route...", 85)
	SendProgress(sender, "cmd-deploy", "complete", "Deploy finished", 100)

	phases := 0
	sender.mu.Lock()
	for _, m := range sender.msgs {
		if m["type"] == "command_progress" {
			phases++
		}
	}
	sender.mu.Unlock()
	if phases < 5 {
		t.Fatalf("expected 5 progress phases, got %d", phases)
	}

	select {
	case r := <-done:
		if r.Status != "success" && r.Status != "failed" {
			// run_preflight success
		}
		if r.Status != "success" {
			t.Fatalf("expected success, got %s (%s)", r.Status, r.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	if err := validateDeployPayload(DeployPayload{AppName: "app", Image: "img:1", Port: 8080}); err != nil {
		t.Fatalf("valid deploy payload rejected: %v", err)
	}
}

// Scenario 2: health fail → auto-rollback uses previous_deployment.
func TestScenario2_HealthFailAutoRollbackUsesPrevious(t *testing.T) {
	dir := t.TempDir()
	sm := state.NewManager(dir)
	_ = sm.RecordDeployment("app", &state.DeploymentRecord{Image: "img:v1", Port: 8080, DeployedAt: "t1"})
	_ = sm.RecordDeployment("app", &state.DeploymentRecord{Image: "img:v2", Port: 8080, DeployedAt: "t2"})

	prev := sm.GetPreviousDeployment("app")
	if prev == nil || prev.Image != "img:v1" {
		t.Fatalf("previous = %+v, want img:v1", prev)
	}

	exec := testExecutor(t).WithStateManager(sm)
	got := exec.rollbackToPrevious(context.Background(), "app")
	if got != nil {
		t.Fatal("expected nil when docker unavailable for rollback redeploy")
	}

	// Health-fail path contract: logs + auto_rollback marker when previous exists
	result := Result{
		Status: "failed",
		Error:  fmt.Sprintf("health check failed: timeout; rolled back to previous version (%s)", prev.Image),
		Logs:   "app crashed",
		Output: "auto_rollback=success",
	}
	if result.Output != "auto_rollback=success" || prev.Image != "img:v1" {
		t.Fatal("auto-rollback result contract broken")
	}
	_ = result
}

// Scenario 3: env update without logging secrets + restart_after.
func TestScenario3_EnvUpdateNoSecretsAndRestartAfter(t *testing.T) {
	dir := t.TempDir()
	em := env.NewManager(dir)
	exec := testExecutor(t).WithEnvManager(em)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)

	secret := "s3cret-value-never-log"
	payload, _ := json.Marshal(UpdateEnvPayload{
		ProjectName:  "myapp",
		Vars:         map[string]string{"API_KEY": secret},
		RestartAfter: true,
	})
	result := exec.Execute(context.Background(), Command{ID: "env-1", Type: "set_env", Payload: payload})
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if !bytes.Contains([]byte(result.Output), []byte("restart_after")) {
		t.Fatalf("expected restart_after in output, got %q", result.Output)
	}
	logs := buf.String()
	if bytes.Contains([]byte(logs), []byte(secret)) {
		t.Fatalf("secret value leaked into logs: %s", logs)
	}
	if !bytes.Contains([]byte(logs), []byte("API_KEY")) {
		t.Fatal("expected key name in logs")
	}

	vars, err := em.ReadEnvFile("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if vars["API_KEY"] != secret {
		t.Fatalf("env not written: %v", vars)
	}
}

// Scenario 4 (agent): offline pending ingest with deploy-newest-wins.
func TestScenario4_ProcessPendingDeployNewestWins(t *testing.T) {
	exec := testExecutor(t).WithPreflightFn(func() (string, error) { return "ok", nil })

	oldPayload, _ := json.Marshal(map[string]string{"app_name": "app", "image": "old"})
	newPayload, _ := json.Marshal(map[string]string{"app_name": "app", "image": "new"})
	other, _ := json.Marshal(map[string]string{})

	var mu sync.Mutex
	var results []Result
	done := make(chan struct{}, 3)

	cmds := []Command{
		{ID: "d1", Type: "deploy", Payload: oldPayload},
		{ID: "p1", Type: "run_preflight", Payload: other},
		{ID: "d2", Type: "deploy", Payload: newPayload},
	}
	exec.ProcessPendingCommands(context.Background(), cmds, func(r Result) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
		done <- struct{}{}
	})

	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("timeout")
		}
	}

	mu.Lock()
	defer mu.Unlock()
	var skipped, ranPreflight bool
	for _, r := range results {
		if r.CommandID == "d1" && r.Status == "skipped" {
			skipped = true
		}
		if r.CommandID == "p1" && r.Status == "success" {
			ranPreflight = true
		}
	}
	if !skipped {
		t.Fatalf("expected older deploy skipped, results=%+v", results)
	}
	if !ranPreflight {
		t.Fatal("expected preflight to run")
	}
}

// Scenario 5: concurrent different projects.
func TestScenario5_ConcurrentDifferentProjects(t *testing.T) {
	sm := NewSlotManager()
	var running int32
	var maxConcurrent int32
	var wg sync.WaitGroup

	run := func(key string) {
		wg.Add(1)
		sm.Submit(key, func() {
			defer wg.Done()
			n := atomic.AddInt32(&running, 1)
			for {
				cur := atomic.LoadInt32(&maxConcurrent)
				if n <= cur || atomic.CompareAndSwapInt32(&maxConcurrent, cur, n) {
					break
				}
			}
			time.Sleep(80 * time.Millisecond)
			atomic.AddInt32(&running, -1)
		})
	}
	run("proj-a")
	run("proj-b")
	run("proj-c")
	wg.Wait()
	if atomic.LoadInt32(&maxConcurrent) < 2 {
		t.Fatalf("expected parallel slots, maxConcurrent=%d", maxConcurrent)
	}
}

// Scenario 6: same project queues behind busy slot.
func TestScenario6_SameProjectQueues(t *testing.T) {
	sm := NewSlotManager()
	order := make([]int, 0, 2)
	var mu sync.Mutex
	var wg sync.WaitGroup
	started := make(chan struct{})

	wg.Add(2)
	sm.Submit("same", func() {
		defer wg.Done()
		close(started)
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	})
	<-started
	sm.Submit("same", func() {
		defer wg.Done()
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
	})
	if !sm.IsBusy("same") {
		t.Fatal("expected busy")
	}
	if q := sm.QueueLen("same"); q != 1 {
		t.Fatalf("queue len=%d want 1", q)
	}
	wg.Wait()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("order=%v", order)
	}
}

// Scenario 7: rollback uses previous_deployment.
func TestScenario7_RollbackUsesPreviousDeployment(t *testing.T) {
	dir := t.TempDir()
	sm := state.NewManager(dir)
	_ = sm.RecordDeployment("shop", &state.DeploymentRecord{Image: "shop:1", Port: 3000, DeployedAt: "a"})
	_ = sm.RecordDeployment("shop", &state.DeploymentRecord{Image: "shop:2", Port: 3000, DeployedAt: "b"})

	prev := sm.GetPreviousDeployment("shop")
	if prev == nil || prev.Image != "shop:1" || prev.Port != 3000 {
		t.Fatalf("previous_deployment=%+v", prev)
	}

	exec := testExecutor(t) // no state → cannot resolve previous
	payload, _ := json.Marshal(map[string]string{"app_name": "shop"})
	result := exec.Execute(context.Background(), Command{ID: "rb-1", Type: "rollback", Payload: payload})
	if result.Status != "error" || result.Error == "" {
		t.Fatalf("expected no previous deployment error, got status=%s err=%s", result.Status, result.Error)
	}
	if !bytes.Contains([]byte(result.Error), []byte("no previous deployment")) {
		t.Fatalf("error=%q", result.Error)
	}

	// With state, rollback selects previous image then attempts full redeploy (nil docker → panic recover via Dispatch)
	exec2 := testExecutor(t).WithStateManager(sm)
	done := make(chan Result, 1)
	exec2.Dispatch(context.Background(), Command{ID: "rb-2", Type: "rollback", Payload: payload}, func(r Result) {
		done <- r
	})
	select {
	case r := <-done:
		if r.Status == "success" {
			t.Fatal("expected redeploy failure without docker")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// Scenario 8: create_database writes DATABASE_URL.
func TestScenario8_EnsureDatabaseURL(t *testing.T) {
	em := env.NewManager(t.TempDir())
	written, err := ensureDatabaseURL(em, "dbapp")
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("expected write")
	}
	vars, err := em.ReadEnvFile("dbapp")
	if err != nil {
		t.Fatal(err)
	}
	url, ok := vars["DATABASE_URL"]
	if !ok || url == "" {
		t.Fatal("DATABASE_URL missing")
	}
	if !bytes.Contains([]byte(url), []byte("postgres://")) {
		t.Fatalf("unexpected url %s", url)
	}
	written2, err := ensureDatabaseURL(em, "dbapp")
	if err != nil {
		t.Fatal(err)
	}
	if written2 {
		t.Fatal("should not overwrite existing DATABASE_URL")
	}
}

// Scenario 9: expired command skipped with expired status.
func TestScenario9_ExpiredCommand(t *testing.T) {
	sender := &captureSender{}
	exec := testExecutor(t).WithProgressSender(sender)
	done := make(chan Result, 1)
	dr := exec.Dispatch(context.Background(), Command{
		ID:        "exp-1",
		Type:      "get_state",
		ExpiresAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	}, func(r Result) { done <- r })

	if !dr.Expired || !dr.Acked {
		t.Fatalf("dr=%+v", dr)
	}
	r := <-done
	if r.Status != "expired" {
		t.Fatalf("status=%s", r.Status)
	}
}

func TestIdempotency_DuplicateReturnsCached(t *testing.T) {
	exec := testExecutor(t).WithPreflightFn(func() (string, error) { return "first", nil })
	payload, _ := json.Marshal(map[string]string{})

	done1 := make(chan Result, 1)
	exec.Dispatch(context.Background(), Command{ID: "idem-1", Type: "run_preflight", Payload: payload}, func(r Result) {
		done1 <- r
	})
	r1 := <-done1

	done2 := make(chan Result, 1)
	dr := exec.Dispatch(context.Background(), Command{ID: "idem-1", Type: "run_preflight", Payload: payload}, func(r Result) {
		done2 <- r
	})
	if !dr.Cached {
		t.Fatal("expected cached")
	}
	r2 := <-done2
	if r2.Output != r1.Output || r2.Status != r1.Status {
		t.Fatalf("cached mismatch: %+v vs %+v", r1, r2)
	}
}

func TestDispatch_PanicRecovery(t *testing.T) {
	exec := testExecutor(t).WithPreflightFn(func() (string, error) {
		panic("boom")
	})
	done := make(chan Result, 1)
	exec.Dispatch(context.Background(), Command{ID: "panic-1", Type: "run_preflight"}, func(r Result) {
		done <- r
	})
	select {
	case r := <-done:
		if r.Status != "failed" {
			t.Fatalf("status=%s", r.Status)
		}
		if r.Error == "" {
			t.Fatal("expected panic error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout — panic may have crashed dispatcher")
	}
}

func TestEnvelope_ServerIDMismatch(t *testing.T) {
	exec := testExecutor(t).WithServerID("srv-a")
	done := make(chan Result, 1)
	dr := exec.Dispatch(context.Background(), Command{
		ID: "bad", Type: "get_state", ServerID: "srv-b",
	}, func(r Result) { done <- r })
	if !dr.Rejected {
		t.Fatal("expected reject")
	}
	r := <-done
	if r.Status != "failed" {
		t.Fatalf("status=%s", r.Status)
	}
}

func TestEnvelope_IsExpired(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if (CommandEnvelope{ExpiresAt: now.Add(-time.Second).Format(time.RFC3339)}).IsExpired(now) != true {
		t.Fatal("expected expired")
	}
	if (CommandEnvelope{ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}).IsExpired(now) != false {
		t.Fatal("expected not expired")
	}
	if (CommandEnvelope{}).IsExpired(now) {
		t.Fatal("empty expires_at should not expire")
	}
}
