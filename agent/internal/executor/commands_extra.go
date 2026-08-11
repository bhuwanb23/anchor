package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/env"
	"github.com/yourname/yourplatform/agent/internal/platform"
)

func (e *Executor) executeStart(ctx context.Context, cmd Command, result *Result) error {
	var p struct {
		ProjectName string `json:"project_name"`
		AppName     string `json:"app_name"`
		ContainerID string `json:"container_id"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid start payload: %w", err)
	}
	id := p.ContainerID
	if id == "" {
		project := p.ProjectName
		if project == "" {
			project = p.AppName
		}
		if e.stateManager != nil {
			if c := e.stateManager.GetProjectAppContainer(project); c != nil {
				id = c.ContainerID
			}
		}
	}
	if id == "" {
		return fmt.Errorf("container_id or project_name required")
	}
	if err := e.docker.StartContainer(ctx, id); err != nil {
		return err
	}
	_ = e.docker.WaitForHealthy(ctx, id, 60*time.Second)
	result.Status = "success"
	result.Output = "started " + id[:12]
	return nil
}

func (e *Executor) executeCreateDatabase(ctx context.Context, cmd Command, result *Result) error {
	var p struct {
		ProjectName string `json:"project_name"`
		Engine      string `json:"engine"`
		Image       string `json:"image,omitempty"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid create_database payload: %w", err)
	}
	if p.ProjectName == "" {
		return fmt.Errorf("project_name is required")
	}
	ct := docker.ContainerTypePostgres
	image := "postgres:16-alpine"
	switch strings.ToLower(p.Engine) {
	case "mysql":
		ct = docker.ContainerTypeMySQL
		image = "mysql:8"
	case "redis":
		ct = docker.ContainerTypeRedis
		image = "redis:7-alpine"
	case "postgres", "":
	default:
		return fmt.Errorf("unsupported engine: %s", p.Engine)
	}
	if p.Image != "" {
		image = p.Image
	}

	payload, _ := json.Marshal(DeployPayload{
		AppName:       p.ProjectName,
		Image:         image,
		ContainerType: ct,
		Name:          p.ProjectName,
	})
	if err := e.executeDeployInternal(ctx, Command{ID: cmd.ID, Type: "deploy", Payload: payload}, result, false); err != nil {
		return err
	}

	if ct == docker.ContainerTypePostgres || ct == docker.ContainerTypeMySQL {
		if written, err := ensureDatabaseURL(e.envManager, p.ProjectName); err != nil {
			return err
		} else if written {
			result.Output += "; DATABASE_URL written to env file"
		}
	}
	return nil
}

// ensureDatabaseURL writes DATABASE_URL to the project env file when missing.
func ensureDatabaseURL(em *env.Manager, projectName string) (written bool, err error) {
	if em == nil {
		return false, nil
	}
	envVars, err := em.ReadEnvFile(projectName)
	if err != nil {
		return false, err
	}
	if envVars == nil {
		envVars = map[string]string{}
	}
	if _, ok := envVars["DATABASE_URL"]; ok {
		return false, nil
	}
	password := generateRandomPassword(24)
	dbName := projectName + "_db"
	envVars["DATABASE_URL"] = env.GenerateDatabaseURL(password, dbName)
	if err := em.WriteEnvFile(projectName, envVars); err != nil {
		return false, err
	}
	return true, nil
}

func (e *Executor) executeDeleteDatabase(ctx context.Context, cmd Command, result *Result) error {
	var p struct {
		ProjectName string `json:"project_name"`
		Engine      string `json:"engine"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid delete_database payload: %w", err)
	}
	role := "postgres"
	switch strings.ToLower(p.Engine) {
	case "mysql":
		role = "mysql"
	case "redis":
		role = "redis"
	}
	name := docker.ContainerName(p.ProjectName, role)
	if _, err := e.docker.ReplaceExistingContainer(ctx, name); err != nil {
		slog.Warn("delete database container", "error", err)
	}
	if e.stateManager != nil {
		_ = e.stateManager.RemoveContainer(p.ProjectName, role)
	}
	result.Status = "success"
	result.Output = "deleted database " + role + " for " + p.ProjectName
	return nil
}

func (e *Executor) executeVerifyDNS(ctx context.Context, cmd Command, result *Result) error {
	var p struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid verify_dns payload: %w", err)
	}
	if p.Domain == "" {
		return fmt.Errorf("domain is required")
	}
	addrs, err := net.LookupHost(p.Domain)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("DNS lookup failed for %s: %v", p.Domain, err)
		return nil
	}
	result.Status = "success"
	result.Output = fmt.Sprintf("%s resolves to %v", p.Domain, addrs)
	return nil
}

// streamJob is a resolved container stream to start.
type streamJob struct {
	role        string
	containerID string
}

// executeStartLogStream handles both start_log_stream (single container_id+role)
// and stream_logs (project_name + containers[]) payloads. One stream per role
// is started; the dashboard can watch several containers at once (Layer 4C 3B).
func (e *Executor) executeStartLogStream(ctx context.Context, cmd Command, result *Result) error {
	if e.logStreamer == nil {
		return fmt.Errorf("log streamer not configured")
	}
	var p struct {
		ProjectName string   `json:"project_name"`
		ContainerID string   `json:"container_id"`
		Role        string   `json:"role"`
		Containers  []string `json:"containers"`
		Tail        int      `json:"tail"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid stream_logs payload: %w", err)
	}
	if p.ProjectName == "" {
		return fmt.Errorf("project_name is required")
	}
	if p.Tail <= 0 {
		p.Tail = 200
	}

	var jobs []streamJob
	if p.ContainerID != "" {
		role := p.Role
		if role == "" {
			role = "app"
		}
		jobs = append(jobs, streamJob{role: role, containerID: p.ContainerID})
	} else {
		roles := p.Containers
		if len(roles) == 0 && p.Role != "" {
			roles = []string{p.Role}
		}
		if len(roles) == 0 {
			roles = []string{"app"}
		}
		for _, role := range roles {
			containerID := ""
			if e.stateManager != nil {
				if c := e.stateManager.GetProjectContainer(p.ProjectName, role); c != nil {
					containerID = c.ContainerID
				}
			}
			jobs = append(jobs, streamJob{role: role, containerID: containerID})
		}
	}

	started := 0
	var errs []string
	for _, j := range jobs {
		if j.containerID == "" {
			errs = append(errs, fmt.Sprintf("no container for role %q", j.role))
			continue
		}
		if err := e.logStreamer.StartStream(ctx, j.containerID, p.ProjectName, j.role, p.Tail); err != nil {
			errs = append(errs, err.Error())
			break // stream limit hit — stop trying further roles
		}
		started++
	}
	if started == 0 {
		if len(errs) > 0 {
			return fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return fmt.Errorf("no containers to stream for project %s", p.ProjectName)
	}
	result.Status = "success"
	result.Output = fmt.Sprintf("streaming %d container(s) for %s", started, p.ProjectName)
	if len(errs) > 0 {
		// Some roles failed (no container or limit hit) — surface it so the
		// control plane knows the start was partial.
		result.Output += "; " + strings.Join(errs, "; ")
	}
	return nil
}

// executeStopLogStream handles stop_stream_logs / stop_log_stream payloads:
//   - all: true            → stop every active stream
//   - project_name         → stop all streams for the project
//   - project_name+containers → stop the listed roles only
//   - container_id         → stop a single stream
func (e *Executor) executeStopLogStream(ctx context.Context, cmd Command, result *Result) error {
	if e.logStreamer == nil {
		return fmt.Errorf("log streamer not configured")
	}
	var p struct {
		ContainerID string   `json:"container_id"`
		ProjectName string   `json:"project_name"`
		Role        string   `json:"role"`
		Containers  []string `json:"containers"`
		All         bool     `json:"all"`
	}
	_ = json.Unmarshal(cmd.Payload, &p)

	if p.All {
		e.logStreamer.StopAll()
		result.Status = "success"
		result.Output = "stopped all log streams"
		return nil
	}
	if p.ProjectName != "" {
		roles := p.Containers
		if len(roles) == 0 && p.Role != "" {
			roles = []string{p.Role}
		}
		if len(roles) == 0 {
			e.logStreamer.StopProject(p.ProjectName)
		} else if e.stateManager != nil {
			for _, role := range roles {
				if c := e.stateManager.GetProjectContainer(p.ProjectName, role); c != nil {
					e.logStreamer.StopStream(c.ContainerID)
				}
			}
		}
		result.Status = "success"
		result.Output = fmt.Sprintf("stopped log streams for %s", p.ProjectName)
		return nil
	}
	if p.ContainerID != "" {
		e.logStreamer.StopStream(p.ContainerID)
	}
	result.Status = "success"
	result.Output = "stopped log stream"
	return nil
}

func (e *Executor) executeUpdateAgent(ctx context.Context, cmd Command, result *Result) error {
	var p struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(cmd.Payload, &p)
	if e.updateFn == nil {
		return fmt.Errorf("update not configured")
	}
	if err := e.updateFn(ctx, p.Version); err != nil {
		return err
	}
	result.Status = "success"
	result.Output = "update applied"
	return nil
}

func (e *Executor) executeRunPreflight(ctx context.Context, cmd Command, result *Result) error {
	if e.preflightFn == nil {
		return fmt.Errorf("preflight not configured")
	}
	out, err := e.preflightFn()
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.Output = out
		return nil
	}
	result.Status = "success"
	result.Output = out
	return nil
}

func (e *Executor) executeGetState(ctx context.Context, cmd Command, result *Result) error {
	if e.stateManager == nil {
		return fmt.Errorf("state manager not configured")
	}
	st := e.stateManager.GetState()
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	result.Status = "success"
	result.Output = string(data)
	return nil
}

func (e *Executor) executeDetectPlatform(ctx context.Context, cmd Command, result *Result) error {
	info := platform.Detect()
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal platform info: %w", err)
	}
	result.Status = "success"
	result.Output = string(data)
	slog.Info("platform detection complete",
		"is_arm64", info.IsArm64,
		"microarchitecture", info.CPU.Microarchitecture,
		"image_tag", info.Build.ImageTag,
		"optimization", info.Build.OptimizationLabel,
	)
	return nil
}
