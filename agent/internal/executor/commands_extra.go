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

func (e *Executor) executeStartLogStream(ctx context.Context, cmd Command, result *Result) error {
	if e.logStreamer == nil {
		return fmt.Errorf("log streamer not configured")
	}
	var p struct {
		StreamID    string `json:"stream_id"`
		ProjectName string `json:"project_name"`
		ContainerID string `json:"container_id"`
		Role        string `json:"role"`
		Tail        int    `json:"tail"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid start_log_stream payload: %w", err)
	}
	if p.Tail <= 0 {
		p.Tail = 200
	}
	role := p.Role
	if role == "" {
		role = "app"
	}
	containerID := p.ContainerID
	if containerID == "" && e.stateManager != nil {
		if c := e.stateManager.GetProjectAppContainer(p.ProjectName); c != nil {
			containerID = c.ContainerID
		}
	}
	if containerID == "" {
		return fmt.Errorf("container_id required")
	}
	streamKey := p.StreamID
	if streamKey == "" {
		streamKey = p.ProjectName + "/" + role
	}
	e.logStreamer.StartStream(ctx, containerID, p.ProjectName, role, p.Tail)
	result.Status = "success"
	result.Output = "streaming " + streamKey
	return nil
}

func (e *Executor) executeStopLogStream(ctx context.Context, cmd Command, result *Result) error {
	if e.logStreamer == nil {
		return fmt.Errorf("log streamer not configured")
	}
	var p struct {
		ContainerID string `json:"container_id"`
		ProjectName string `json:"project_name"`
		Role        string `json:"role"`
	}
	_ = json.Unmarshal(cmd.Payload, &p)
	id := p.ContainerID
	if id == "" && e.stateManager != nil {
		if c := e.stateManager.GetProjectAppContainer(p.ProjectName); c != nil {
			id = c.ContainerID
		}
	}
	if id != "" {
		e.logStreamer.StopStream(id)
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
