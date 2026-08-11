//go:build linux

package platform

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// detectPlatform performs the full platform detection on Linux.
func detectPlatform() *PlatformInfo {
	info := &PlatformInfo{
		CPU: CPUInfo{
			DetectionConfidence: "low",
		},
	}

	detectCPUInfo(info)
	detectCPUFeatures(info)
	detectCloudHint(info)
	detectMemory(info)
	detectDisk(info)
	selectBuild(info)
	assessReadiness(info)

	return info
}

// parseCPUInfoBlock reads /proc/cpuinfo and returns a slice of blocks,
// where each block is a map of field → value for one logical processor.
func parseCPUInfoBlock() []map[string]string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return nil
	}
	defer f.Close()

	var blocks []map[string]string
	current := make(map[string]string)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = make(map[string]string)
			}
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			current[key] = val
		}
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	return blocks
}

// detectCPUInfo extracts CPU identity from /proc/cpuinfo.
func detectCPUInfo(info *PlatformInfo) {
	blocks := parseCPUInfoBlock()
	if len(blocks) == 0 {
		return
	}

	first := blocks[0]
	info.CPU.Cores = len(blocks)

	if model, ok := first["model name"]; ok {
		info.CPU.ModelName = model
	} else if model, ok := first["Model"]; ok {
		info.CPU.ModelName = model
	}

	if vendor, ok := first["CPU implementer"]; ok {
		info.CPU.VendorID = strings.TrimSpace(vendor)
	}

	// Check if this is ARM64 by looking for the CPU architecture field
	// or by checking runtime.GOARCH.
	if runtime.GOARCH == "arm64" {
		info.IsArm64 = true
	} else if arch, ok := first["CPU architecture"]; ok {
		// "CPU architecture: 8" means ARMv8 (AArch64)
		if strings.TrimSpace(arch) == "8" {
			info.IsArm64 = true
		}
	}

	if !info.IsArm64 {
		// Fall back to uname -m for runtime detection
		if out, err := exec.Command("uname", "-m").Output(); err == nil {
			m := strings.TrimSpace(string(out))
			if m == "aarch64" || m == "arm64" {
				info.IsArm64 = true
			}
		}
	}

	// Extract CPU part code (ARM-specific)
	if part, ok := first["CPU part"]; ok {
		partCode := strings.TrimSpace(strings.ToLower(part))
		info.CPU.CPUPartCode = partCode
		if name, ok := armCPUPartCodes[partCode]; ok {
			info.CPU.Microarchitecture = name
			info.CPU.DetectionConfidence = "high"
		}
	}

	// Extract clock speed
	if mhz, ok := first["cpu MHz"]; ok {
		if v, err := strconv.ParseFloat(strings.TrimSpace(mhz), 64); err == nil {
			info.CPU.Mhz = v
		}
	} else if mhz, ok := first["BogoMIPS"]; ok {
		// On ARM, BogoMIPS is roughly the clock speed
		if v, err := strconv.ParseFloat(strings.TrimSpace(mhz), 64); err == nil {
			info.CPU.Mhz = v
		}
	}
}

// detectCPUFeatures parses the Features line from /proc/cpuinfo.
func detectCPUFeatures(info *PlatformInfo) {
	blocks := parseCPUInfoBlock()
	if len(blocks) == 0 {
		return
	}

	featuresLine := ""
	for _, block := range blocks {
		if f, ok := block["Features"]; ok {
			featuresLine = f
			break
		}
	}
	if featuresLine == "" {
		return
	}

	featureSet := make(map[string]bool)
	for _, f := range strings.Fields(featuresLine) {
		featureSet[strings.ToLower(f)] = true
	}

	info.Features = CPUFeatures{
		Dotprod: featureSet["dotprod"],
		I8mm:    featureSet["i8mm"],
		Sve:     featureSet["sve"],
		Sve2:    featureSet["sve2"],
		Bf16:    featureSet["bf16"],
	}
}

// detectCloudHint reads DMI data and hostname to guess the cloud provider.
func detectCloudHint(info *PlatformInfo) {
	boardVendor := readDMI("/sys/class/dmi/id/board_vendor")
	chassisVendor := readDMI("/sys/class/dmi/id/chassis_vendor")

	hostname := ""
	if h, err := os.Hostname(); err == nil {
		hostname = h
	}

	hint := cloudProviderHints(boardVendor, chassisVendor, hostname)
	if hint != "" {
		info.CPU.CloudProviderHint = hint
		if info.CPU.DetectionConfidence != "high" {
			info.CPU.DetectionConfidence = "low"
		}
	}
}

// readDMI reads a single DMI sysfs file, returning "" on any error.
func readDMI(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// detectMemory reads RAM from gopsutil and applies the Step 4 decision table.
func detectMemory(info *PlatformInfo) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return
	}

	totalMB := int64(v.Total / 1024 / 1024)
	availMB := int64(v.Available / 1024 / 1024)
	availGB := float64(availMB) / 1024.0

	info.Memory = MemoryInfo{
		TotalMB:      totalMB,
		AvailableMB:  availMB,
		AvailableGB:  math.Round(availGB*10) / 10,
	}

	// Step 4 decision table: Available RAM → model + quantization recommendation
	switch {
	case availGB > 14:
		// Comfortable: 7B Q4_K_M with 6GB headroom
		info.Memory.RecommendedModel = "7b"
		info.Memory.RecommendedQuant = "Q4_K_M"
		info.Memory.MemorySufficient = true
	case availGB >= 8:
		// Tight: 7B Q4_K_M minimum config
		info.Memory.RecommendedModel = "7b"
		info.Memory.RecommendedQuant = "Q4_K_M"
		info.Memory.MemorySufficient = true
		info.Memory.MemoryNote = "Memory is limited. Close other apps before deploying."
	case availGB >= 4:
		// Too tight for 7B: use 3B
		info.Memory.RecommendedModel = "3b"
		info.Memory.RecommendedQuant = "Q4_K_M"
		info.Memory.MemorySufficient = true
		info.Memory.MemoryNote = "Your server has limited memory. Using a smaller model."
	default:
		// Block: less than 4GB available
		info.Memory.RecommendedModel = ""
		info.Memory.RecommendedQuant = ""
		info.Memory.MemorySufficient = false
	}
}

// detectDisk reads disk usage for the root filesystem and checks against
// the estimated model file size.
func detectDisk(info *PlatformInfo) {
	usage, err := disk.Usage("/")
	if err != nil {
		return
	}

	availGB := float64(usage.Free) / 1024 / 1024 / 1024
	info.Disk = DiskInfo{
		TotalGB:     float64(usage.Total) / 1024 / 1024 / 1024,
		AvailableGB: math.Round(availGB*10) / 10,
	}

	// Estimate model file size based on recommended model
	var modelSizeGB float64
	switch info.Memory.RecommendedModel {
	case "7b":
		modelSizeGB = modelSize7BQ4KM
	case "3b":
		modelSizeGB = modelSize3BQ4KM
	default:
		modelSizeGB = modelSize7BQ4KM // default check against largest
	}

	info.Disk.ModelRequiredGB = math.Round((modelSizeGB+modelBufferGB)*10) / 10
	info.Disk.DiskSufficient = info.Disk.AvailableGB >= info.Disk.ModelRequiredGB
}

// selectBuild picks the Docker image tag and optimization label (Step 3).
// Decision logic evaluated in order, first match wins.
func selectBuild(info *PlatformInfo) {
	if !info.IsArm64 {
		info.Build = BuildSelection{
			ImageTag:         InferImageBase + ":x86_64",
			OptimizationLabel: "No Arm optimization",
			ExpectedHardware: "x86_64 server",
		}
		return
	}

	f := info.Features

	switch {
	case f.Sve2 && f.I8mm:
		info.Build = BuildSelection{
			ImageTag:         InferImageBase + ":arm64-sve2-i8mm",
			OptimizationLabel: "Maximum (SVE2 + I8MM)",
			ExpectedHardware: "Graviton 4, GCP Axion",
		}
	case f.Sve && f.I8mm:
		info.Build = BuildSelection{
			ImageTag:         InferImageBase + ":arm64-i8mm-sve",
			OptimizationLabel: "Full (SVE + I8MM)",
			ExpectedHardware: "Graviton 3",
		}
	case f.I8mm:
		info.Build = BuildSelection{
			ImageTag:         InferImageBase + ":arm64-i8mm",
			OptimizationLabel: "High (I8MM)",
			ExpectedHardware: "Azure Cobalt, Ampere Altra",
		}
	case f.Dotprod:
		info.Build = BuildSelection{
			ImageTag:         InferImageBase + ":arm64-dotprod",
			OptimizationLabel: "Basic (DOTPROD)",
			ExpectedHardware: "older Arm64 servers",
		}
	default:
		info.Build = BuildSelection{
			ImageTag:         InferImageBase + ":arm64",
			OptimizationLabel: "Generic (Arm64)",
			ExpectedHardware: "Arm64 server",
		}
	}
}

// assessReadiness evaluates memory and disk to determine if inference
// can proceed and what notes/warnings to show.
func assessReadiness(info *PlatformInfo) {
	var notes []string
	var blockReason string
	canRun := true

	// Memory check
	if !info.Memory.MemorySufficient {
		canRun = false
		blockReason = fmt.Sprintf(
			"AI inference requires at least 4GB of available memory. Your server has %.1fGB available. Stop other running apps or upgrade to a larger server.",
			info.Memory.AvailableGB,
		)
	} else if info.Memory.MemoryNote != "" {
		notes = append(notes, info.Memory.MemoryNote)
	}

	// Disk check
	if !info.Disk.DiskSufficient {
		canRun = false
		if blockReason != "" {
			blockReason += " "
		}
		blockReason += fmt.Sprintf(
			"Downloading this model requires %.1fGB of free disk space. You have %.1fGB available. Free up space or choose a smaller model.",
			info.Disk.ModelRequiredGB, info.Disk.AvailableGB,
		)
	}

	// x86 note (not a block, just a note)
	if !info.IsArm64 {
		notes = append(notes, "For best results, use an Arm64 server.")
	}

	info.Readiness = Readiness{
		CanRunInference: canRun,
		BlockReason:     blockReason,
		Notes:           notes,
	}
}
