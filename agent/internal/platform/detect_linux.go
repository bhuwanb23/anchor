//go:build linux

package platform

import (
	"bufio"
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
	detectRecommendations(info)

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
		// Upgrade confidence if we have a CPU part match, otherwise keep low
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

// detectMemory reads RAM from gopsutil and sets the recommended model size.
func detectMemory(info *PlatformInfo) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return
	}

	info.Memory = MemoryInfo{
		TotalMB:     int64(v.Total / 1024 / 1024),
		AvailableMB: int64(v.Available / 1024 / 1024),
	}
}

// detectDisk reads disk usage for the root filesystem.
func detectDisk(info *PlatformInfo) {
	usage, err := disk.Usage("/")
	if err != nil {
		return
	}

	info.Disk = DiskInfo{
		TotalGB:     float64(usage.Total) / 1024 / 1024 / 1024,
		AvailableGB: float64(usage.Free) / 1024 / 1024 / 1024,
	}
}

// detectRecommendations sets the recommended llama.cpp build and quantization.
func detectRecommendations(info *PlatformInfo) {
	info.RecommendedBuild = recommendBuild(info.Features)
	info.RecommendedQuant = recommendQuantization(info.Memory.TotalMB, info.Features)
}

// recommendBuild picks the best llama.cpp build variant for the detected CPU.
func recommendBuild(f CPUFeatures) string {
	if f.I8mm && f.Sve {
		return "arm64-i8mm-sve"
	}
	if f.I8mm {
		return "arm64-i8mm"
	}
	if f.Dotprod {
		return "arm64-dotprod"
	}
	return "arm64"
}

// recommendQuantization picks a default quantization level based on
// available RAM and CPU capabilities.
func recommendQuantization(ramMB int64, f CPUFeatures) string {
	// I8mm accelerates INT8 quantization — prefer higher quality quants
	if f.I8mm && ramMB >= 16000 {
		return "Q4_K_M"
	}
	if f.I8mm && ramMB >= 8000 {
		return "Q4_K_S"
	}
	if ramMB >= 8000 {
		return "Q4_K_S"
	}
	if ramMB >= 4000 {
		return "Q3_K_M"
	}
	return "Q2_K"
}
