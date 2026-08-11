package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/platform"
	"github.com/yourname/yourplatform/agent/internal/state"
)

// executeDeployInference deploys an inference server using a template.
// It reads the template, selects the right quantization and image based
// on the server's platform, pulls the model, starts the container, and
// sets up a Caddy route for HTTPS access.
func (e *Executor) executeDeployInference(ctx context.Context, cmd Command, result *Result) error {
	var p DeployInferencePayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid deploy_inference payload: %w", err)
	}

	if p.TemplateID == "" {
		return fmt.Errorf("template_id is required")
	}

	// Look up template
	if e.inferRegistry == nil {
		return fmt.Errorf("inference template registry not configured")
	}
	tmpl := e.inferRegistry.Get(p.TemplateID)
	if tmpl == nil {
		return fmt.Errorf("template not found: %s", p.TemplateID)
	}

	slog.Info("deploy_inference started",
		"template", tmpl.ID,
		"model", tmpl.Model.Family+" "+tmpl.Model.Size,
	)

	// Step 1: Detect platform
	SendProgress(e.progressSender, cmd.ID, "detecting", "Detecting server platform...", 5)
	plat := platform.Detect()

	// Step 2: Select quantization based on available RAM
	availableRAMGB := float64(plat.Memory.AvailableMB) / 1024.0
	quantKey, quantInfo := tmpl.SelectQuantization(availableRAMGB)
	if quantInfo == nil {
		return fmt.Errorf("no suitable quantization found for %.1fGB RAM", availableRAMGB)
	}

	if !plat.Readiness.CanRunInference {
		return fmt.Errorf("server not ready: %s", plat.Readiness.BlockReason)
	}

	slog.Info("selected quantization",
		"quant", quantKey,
		"file", quantInfo.FileName,
		"size_gb", quantInfo.SizeGB,
		"available_ram_gb", availableRAMGB,
	)

	// Step 3: Determine the Docker image tag from platform detection
	imageTag := plat.Build.ImageTag
	SendProgress(e.progressSender, cmd.ID, "preparing", fmt.Sprintf("Using optimized build: %s", plat.Build.OptimizationLabel), 10)

	// Step 4: Pull the inference server image
	SendProgress(e.progressSender, cmd.ID, "pulling", "Pulling inference server image...", 15)
	if e.docker != nil {
		var progressFn docker.PullProgressFunc
		if e.reporter != nil {
			progressFn = func(p docker.PullProgress) error {
				e.reporter.ReportProgress(p)
				return nil
			}
		}
		if _, _, err := e.docker.PullImageIfNeeded(ctx, imageTag, e.imageCache, progressFn); err != nil {
			return fmt.Errorf("pull inference image: %w", err)
		}
	}

	// Step 5: Prepare model download directory
	SendProgress(e.progressSender, cmd.ID, "downloading", "Preparing model storage...", 40)
	modelDir := fmt.Sprintf("/var/lib/yourplatform/models/%s", p.TemplateID)

	// Step 6: Create and start the container
	SendProgress(e.progressSender, cmd.ID, "creating", "Creating inference container...", 45)

	containerName := fmt.Sprintf("infer-%s", p.TemplateID)
	if e.docker != nil {
		_, _ = e.docker.ReplaceExistingContainer(ctx, containerName)
	}

	// Build port mapping
	portSpec := &docker.AppPortSpec{
		ContainerPort: tmpl.Runtime.InternalPort,
		BindAddress:   "127.0.0.1",
	}
	portMap, exposedPorts := docker.PortMapping(docker.ContainerTypeApp, portSpec)

	// Resource limits based on platform
	memLimitMB := int64(plat.Memory.AvailableMB * 80 / 100) // use 80% of available
	if memLimitMB < 2048 {
		memLimitMB = 2048
	}
	rlimits := &docker.ResourceLimits{
		MemoryHard: memLimitMB * 1024 * 1024,
		MemorySoft: memLimitMB * 512 * 1024, // 50% of hard as soft
	}

	// Environment variables for the inference container
	envVars := map[string]string{
		"MODEL_DIR":     modelDir,
		"MODEL_FILE":    quantInfo.FileName,
		"CONTEXT_SIZE":  fmt.Sprintf("%d", tmpl.Runtime.ContextWindow),
		"MAX_CONCURRENT": fmt.Sprintf("%d", tmpl.Runtime.MaxConcurrent),
	}
	if p.APIKey != "" {
		envVars["API_KEY"] = p.APIKey
	}

	var containerID string
	if e.docker != nil {
		id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
			Name:           containerName,
			Image:          imageTag,
			PortBindings:   portMap,
			ExposedPorts:   exposedPorts,
			Env:            formatEnvVars(envVars),
			Labels: map[string]string{
				"yourplatform.type":     "inference",
				"yourplatform.template": tmpl.ID,
				"yourplatform.quant":    quantKey,
			},
			ResourceLimits: rlimits,
			RestartPolicy:  "unless-stopped",
		})
		if err != nil {
			return fmt.Errorf("create container: %w", err)
		}
		containerID = id
	}

	SendProgress(e.progressSender, cmd.ID, "starting", "Starting inference server...", 55)

	if e.docker != nil && containerID != "" {
		if err := e.docker.StartContainerWithWait(ctx, containerID, 60*time.Second); err != nil {
			return fmt.Errorf("start container: %w", err)
		}
	}

	// Step 7: Wait for the server to be healthy
	SendProgress(e.progressSender, cmd.ID, "health", "Waiting for inference server to be ready...", 65)
	if e.docker != nil && containerID != "" {
		_ = e.docker.WaitForHealthy(ctx, containerID, 120*time.Second)
	}

	// Step 8: Set up Caddy route for HTTPS
	if p.Domain != "" && e.caddy != nil {
		SendProgress(e.progressSender, cmd.ID, "routing", "Setting up HTTPS route...", 80)
		routeID := "infer-" + p.TemplateID
		upstream := fmt.Sprintf("127.0.0.1:%d", tmpl.Runtime.InternalPort)
		domains := []string{p.Domain}
		if err := e.caddy.SetRouteByID(routeID, domains, upstream); err != nil {
			slog.Warn("failed to set Caddy route", "error", err)
		}
	}

	// Step 9: Persist state
	if e.stateManager != nil {
		_ = e.stateManager.SetContainer(p.TemplateID, "inference", &state.ContainerState{
			ContainerID:   containerID,
			Image:         imageTag,
			Status:        "running",
			Domain:        p.Domain,
			HostPort:      tmpl.Runtime.InternalPort,
			RestartPolicy: "unless-stopped",
		})
	}

	// Done
	SendProgress(e.progressSender, cmd.ID, "complete", "Inference server deployed successfully!", 100)

	output, _ := json.Marshal(map[string]interface{}{
		"template_id":     tmpl.ID,
		"container_id":    containerID,
		"image_tag":       imageTag,
		"quantization":    quantKey,
		"model_file":      quantInfo.FileName,
		"internal_port":   tmpl.Runtime.InternalPort,
		"domain":          p.Domain,
		"api_path":        tmpl.Runtime.APIPath,
		"optimization":    plat.Build.OptimizationLabel,
		"memory_limit_mb": memLimitMB,
	})
	result.Status = "success"
	result.Output = string(output)

	slog.Info("deploy_inference complete",
		"template", tmpl.ID,
		"container", containerID,
		"quant", quantKey,
		"domain", p.Domain,
	)
	return nil
}

// formatEnvVars converts a map to Docker's KEY=VALUE slice format.
func formatEnvVars(m map[string]string) []string {
	result := make([]string, 0, len(m))
	for k, v := range m {
		result = append(result, k+"="+v)
	}
	return result
}
