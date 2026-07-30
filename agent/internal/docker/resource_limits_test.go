package docker

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Default resource limits
// ---------------------------------------------------------------------------

func TestDefaultResourceLimits_App(t *testing.T) {
	rl := DefaultResourceLimits(ContainerTypeApp)
	if rl == nil {
		t.Fatal("expected non-nil limits")
	}
	if rl.MemorySoft != 256*mb {
		t.Errorf("expected soft limit 256MB, got %s", formatBytes(rl.MemorySoft))
	}
	if rl.MemoryHard != 512*mb {
		t.Errorf("expected hard limit 512MB, got %s", formatBytes(rl.MemoryHard))
	}
	if rl.MemorySwap != 0 {
		t.Errorf("expected swap 0, got %d", rl.MemorySwap)
	}
	if rl.CPUShares != 512 {
		t.Errorf("expected CPU shares 512, got %d", rl.CPUShares)
	}
}

func TestDefaultResourceLimits_Postgres(t *testing.T) {
	rl := DefaultResourceLimits(ContainerTypePostgres)
	if rl.MemorySoft != 512*mb {
		t.Errorf("expected soft limit 512MB, got %s", formatBytes(rl.MemorySoft))
	}
	if rl.MemoryHard != 1*gb {
		t.Errorf("expected hard limit 1GB, got %s", formatBytes(rl.MemoryHard))
	}
}

func TestDefaultResourceLimits_MySQL(t *testing.T) {
	rl := DefaultResourceLimits(ContainerTypeMySQL)
	if rl.MemorySoft != 512*mb {
		t.Errorf("expected soft limit 512MB, got %s", formatBytes(rl.MemorySoft))
	}
}

func TestDefaultResourceLimits_Redis(t *testing.T) {
	rl := DefaultResourceLimits(ContainerTypeRedis)
	if rl.MemorySoft != 128*mb {
		t.Errorf("expected soft limit 128MB, got %s", formatBytes(rl.MemorySoft))
	}
	if rl.MemoryHard != 256*mb {
		t.Errorf("expected hard limit 256MB, got %s", formatBytes(rl.MemoryHard))
	}
}

// ---------------------------------------------------------------------------
// Validate resource limits
// ---------------------------------------------------------------------------

func TestValidateResourceLimits_Valid(t *testing.T) {
	rl := &ResourceLimits{
		MemorySoft: 256 * mb,
		MemoryHard: 512 * mb,
		MemorySwap: 0,
		CPUShares:  512,
	}
	err := ValidateResourceLimits(rl, 2048) // 2GB total
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateResourceLimits_SoftExceedsHard(t *testing.T) {
	rl := &ResourceLimits{
		MemorySoft: 1 * gb,
		MemoryHard: 512 * mb, // soft > hard
	}
	err := ValidateResourceLimits(rl, 4096)
	if err == nil {
		t.Fatal("expected error when soft > hard")
	}
}

func TestValidateResourceLimits_ExceedsTotalRAM(t *testing.T) {
	rl := &ResourceLimits{
		MemoryHard: 2 * gb, // exceeds 1GB total - 512MB system = 512MB available
	}
	err := ValidateResourceLimits(rl, 1024) // 1GB total
	if err == nil {
		t.Fatal("expected error when limit exceeds available")
	}
}

func TestValidateResourceLimits_EdgeCase(t *testing.T) {
	rl := &ResourceLimits{
		MemoryHard: 512 * mb, // exactly 1GB - 512MB system
	}
	err := ValidateResourceLimits(rl, 1024)
	if err != nil {
		t.Errorf("expected no error at boundary, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Format bytes
// ---------------------------------------------------------------------------

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1KB"},
		{1 * mb, "1MB"},
		{512 * mb, "512MB"},
		{1 * gb, "1.0GB"},
		{1536 * mb, "1.5GB"},
	}

	for _, c := range cases {
		got := formatBytes(c.input)
		if got != c.want {
			t.Errorf("formatBytes(%d) = %s, want %s", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ToDockerResources
// ---------------------------------------------------------------------------

func TestToDockerResources(t *testing.T) {
	rl := &ResourceLimits{
		MemorySoft: 256 * mb,
		MemoryHard: 512 * mb,
		MemorySwap: 0,
		CPUShares:  512,
	}

	res := rl.ToDockerResources()
	if res.Memory != 512*mb {
		t.Errorf("expected Memory=%d, got %d", 512*mb, res.Memory)
	}
	if res.MemoryReservation != 256*mb {
		t.Errorf("expected MemoryReservation=%d, got %d", 256*mb, res.MemoryReservation)
	}
	if res.CPUShares != 512 {
		t.Errorf("expected CPUShares=512, got %d", res.CPUShares)
	}
}
