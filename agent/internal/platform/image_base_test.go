package platform

import "testing"

func TestImageBaseDefault(t *testing.T) {
	t.Setenv("ANCHOR_INFER_IMAGE_BASE", "")
	if got := ImageBase(); got != DefaultInferImageBase {
		t.Fatalf("ImageBase() = %q, want %q", got, DefaultInferImageBase)
	}
}

func TestImageBaseOverride(t *testing.T) {
	t.Setenv("ANCHOR_INFER_IMAGE_BASE", "ghcr.io/example/anchor-infer")
	if got := ImageBase(); got != "ghcr.io/example/anchor-infer" {
		t.Fatalf("ImageBase() = %q, want override", got)
	}
}
