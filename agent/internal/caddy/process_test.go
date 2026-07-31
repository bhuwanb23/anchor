package caddy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProcessConfig_Defaults(t *testing.T) {
	cfg := ProcessConfig{}
	cfg.defaults()

	if cfg.BinaryPath != defaultBinaryPath {
		t.Errorf("default BinaryPath = %q, want %q", cfg.BinaryPath, defaultBinaryPath)
	}
	if cfg.DataDir != defaultDataDir {
		t.Errorf("default DataDir = %q, want %q", cfg.DataDir, defaultDataDir)
	}
	if cfg.AdminURL != "http://localhost:2019" {
		t.Errorf("default AdminURL = %q, want http://localhost:2019", cfg.AdminURL)
	}
	if cfg.ACMEmail != "certs@yourplatform.com" {
		t.Errorf("default ACMEmail = %q, want certs@yourplatform.com", cfg.ACMEmail)
	}
	if !cfg.UseStagingEnabled() {
		t.Error("default UseStaging should be true")
	}
}

func TestProcessConfig_PidFile(t *testing.T) {
	cfg := ProcessConfig{DataDir: "/var/lib/yourplatform/caddy"}
	got := cfg.pidFile()
	want := filepath.Join(cfg.DataDir, "caddy.pid")
	if got != want {
		t.Errorf("pidFile() = %q, want %q", got, want)
	}
}

func TestProcessConfig_ConfigFile(t *testing.T) {
	cfg := ProcessConfig{DataDir: "/var/lib/yourplatform/caddy"}
	got := cfg.configFile()
	want := filepath.Join(cfg.DataDir, "config.json")
	if got != want {
		t.Errorf("configFile() = %q, want %q", got, want)
	}
}

func TestProcessManager_PIDFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir, BinaryPath: "/nonexistent", AdminURL: "http://localhost:19999"}
	pm := NewProcessManager(cfg, nil)

	if err := pm.writePIDFile(12345); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}

	pid, err := pm.readPIDFile()
	if err != nil {
		t.Fatalf("readPIDFile: %v", err)
	}
	if pid != 12345 {
		t.Errorf("readPIDFile = %d, want 12345", pid)
	}

	if _, err := os.Stat(pm.cfg.pidFile()); os.IsNotExist(err) {
		t.Error("PID file should exist")
	}

	pm.removePIDFile()
	if _, err := os.Stat(pm.cfg.pidFile()); !os.IsNotExist(err) {
		t.Error("PID file should be removed")
	}
}

func TestProcessManager_PIDFileCorrupt(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "caddy.pid")
	os.WriteFile(pidFile, []byte("not-a-number"), 0644)

	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, nil)

	_, err := pm.readPIDFile()
	if err == nil {
		t.Error("readPIDFile should fail on corrupt data")
	}
}

func TestProcessManager_IsProcessAlive_Nonexistent(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, nil)

	if pm.isProcessAlive(99999999) {
		t.Error("nonexistent PID should not be alive")
	}
}

func TestProcessManager_IsAlive_NoPIDFile(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, nil)

	if pm.IsAlive() {
		t.Error("IsAlive should return false with no PID file")
	}
}

func TestProcessManager_EnsureConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir, BinaryPath: "/nonexistent", AdminURL: "http://localhost:19999"}
	pm := NewProcessManager(cfg, nil)

	if err := pm.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}

	data, err := os.ReadFile(cfg.configFile())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	s := string(data)
	// Admin API
	if !strings.Contains(s, "localhost:2019") {
		t.Error("config should contain admin listen")
	}
	// HTTPS server on :443
	if !strings.Contains(s, ":443") {
		t.Error("config should contain HTTPS listen :443")
	}
	// HTTP redirect server on :80
	if !strings.Contains(s, ":80") {
		t.Error("config should contain HTTP redirect listen :80")
	}
	// ACME email
	if !strings.Contains(s, "certs@yourplatform.com") {
		t.Error("config should contain ACME email")
	}
	// Staging ACME URL
	if !strings.Contains(s, "acme-staging-v02") {
		t.Error("config should contain staging ACME URL by default")
	}
	// TLS config
	if !strings.Contains(s, `"tls"`) {
		t.Error("config should contain tls app")
	}
	// Redirect handler
	if !strings.Contains(s, "static_response") {
		t.Error("config should contain redirect handler")
	}
	// Server names
	if !strings.Contains(s, `"main"`) {
		t.Error("config should contain main server")
	}
	if !strings.Contains(s, `"redirect"`) {
		t.Error("config should contain redirect server")
	}

	// Calling again should not overwrite
	if err := pm.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig second call: %v", err)
	}
	data2, _ := os.ReadFile(cfg.configFile())
	if string(data) != string(data2) {
		t.Error("ensureConfig should not overwrite existing config")
	}
}

func TestProcessManager_EnsureConfig_Production(t *testing.T) {
	dir := t.TempDir()
	f := false
	cfg := ProcessConfig{
		DataDir:    dir,
		BinaryPath: "/nonexistent",
		AdminURL:   "http://localhost:19999",
		UseStaging: &f,
	}
	pm := NewProcessManager(cfg, nil)

	if err := pm.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}

	data, _ := os.ReadFile(cfg.configFile())
	s := string(data)
	if strings.Contains(s, "acme-staging-v02") {
		t.Error("production config should not use staging ACME URL")
	}
	if !strings.Contains(s, "acme-v02.api.letsencrypt.org") {
		t.Error("production config should use production ACME URL")
	}
}

func TestProcessManager_EnsureConfig_CertDir(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certs")
	cfg := ProcessConfig{
		DataDir:  dir,
		CertDir:  certDir,
	}
	pm := NewProcessManager(cfg, nil)

	if err := pm.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}

	if _, err := os.Stat(certDir); os.IsNotExist(err) {
		t.Error("certificate directory should be created")
	}

	data, _ := os.ReadFile(cfg.configFile())
	// JSON escapes backslashes on Windows, so check the dir name is present
	if !strings.Contains(string(data), "certs") {
		t.Error("config should contain cert directory")
	}
}

func TestProcessManager_BinaryNotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{
		DataDir:    dir,
		BinaryPath: filepath.Join(dir, "nonexistent-caddy"),
		AdminURL:   "http://localhost:19999",
	}
	pm := NewProcessManager(cfg, nil)

	if err := pm.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig should work: %v", err)
	}

	if pm.IsAlive() {
		t.Error("should not be alive with no PID file")
	}
}

func TestProcessManager_Start_BinaryMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{
		DataDir:    dir,
		BinaryPath: filepath.Join(dir, "no-caddy"),
		AdminURL:   "http://localhost:19999",
	}
	pm := NewProcessManager(cfg, nil)

	err := pm.Start(t.Context())
	if err == nil {
		t.Error("Start should fail when binary is missing")
	}
}

func TestReadPIDFile_NotExist(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, nil)

	_, err := pm.readPIDFile()
	if err == nil {
		t.Error("readPIDFile should fail when file doesn't exist")
	}
}

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Signal(0) not reliable on Windows")
	}
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, nil)

	if !pm.isProcessAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

func TestNewProcessManager(t *testing.T) {
	m := NewManager("http://localhost:2019")
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, m)

	if pm.cfg.DataDir != dir {
		t.Errorf("DataDir = %q, want %q", pm.cfg.DataDir, dir)
	}
	if pm.manager != m {
		t.Error("manager should be set")
	}
}

func TestProcessConfig_ExpandedDefaults(t *testing.T) {
	cfg := ProcessConfig{
		BinaryPath: "/usr/bin/caddy",
		DataDir:    "/opt/caddy",
		AdminURL:   "http://localhost:9090",
		ACMEmail:   "admin@example.com",
		CertDir:    "/opt/caddy/certs",
	}
	cfg.defaults()

	if cfg.BinaryPath != "/usr/bin/caddy" {
		t.Errorf("BinaryPath should be preserved, got %q", cfg.BinaryPath)
	}
	if cfg.DataDir != "/opt/caddy" {
		t.Errorf("DataDir should be preserved, got %q", cfg.DataDir)
	}
	if cfg.AdminURL != "http://localhost:9090" {
		t.Errorf("AdminURL should be preserved, got %q", cfg.AdminURL)
	}
	if cfg.ACMEmail != "admin@example.com" {
		t.Errorf("ACMEmail should be preserved, got %q", cfg.ACMEmail)
	}
	if cfg.CertDir != "/opt/caddy/certs" {
		t.Errorf("CertDir should be preserved, got %q", cfg.CertDir)
	}
}

func TestEnsureConfig_ContainsValidJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir, BinaryPath: "/nonexistent", AdminURL: "http://localhost:19999"}
	pm := NewProcessManager(cfg, nil)

	if err := pm.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}

	data, err := os.ReadFile(cfg.configFile())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	s := string(data)
	for _, key := range []string{`"admin"`, `"apps"`, `"http"`, `"tls"`, `"main"`, `"redirect"`, `"listen"`, `"routes"`, `"storage"`, `"acme"`} {
		if !strings.Contains(s, key) {
			t.Errorf("config missing key %s", key)
		}
	}
}

func TestPIDFile_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not applicable on Windows")
	}
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, nil)

	if err := pm.writePIDFile(os.Getpid()); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}

	info, err := os.Stat(pm.cfg.pidFile())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0644 {
		t.Errorf("PID file permissions = %o, want 0644", perm)
	}
}

func TestEnsureConfig_CreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "caddy")
	cfg := ProcessConfig{DataDir: dir, BinaryPath: "/nonexistent", AdminURL: "http://localhost:19999"}
	pm := NewProcessManager(cfg, nil)

	if err := pm.ensureConfig(); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("data directory should be created")
	}
}

func TestProcessManager_Stop_NoPIDFile(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir}
	pm := NewProcessManager(cfg, nil)

	if err := pm.Stop(); err != nil {
		t.Errorf("Stop should not fail: %v", err)
	}
}

func TestProcessManager_Restart_NoRoutes(t *testing.T) {
	dir := t.TempDir()
	cfg := ProcessConfig{DataDir: dir, BinaryPath: filepath.Join(dir, "no-caddy"), AdminURL: "http://localhost:19999"}
	pm := NewProcessManager(cfg, nil)

	err := pm.Restart(t.Context(), nil)
	if err == nil {
		t.Error("Restart should fail when binary is missing")
	}
}
