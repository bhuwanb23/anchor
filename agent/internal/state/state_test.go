package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourname/yourplatform/agent/internal/caddy"
)

func TestNewState(t *testing.T) {
	s := NewState()
	if s.Version != StateVersion {
		t.Errorf("expected version %d, got %d", StateVersion, s.Version)
	}
	if len(s.Projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(s.Projects))
	}
	if len(s.Routes) != 0 {
		t.Errorf("expected empty routes, got %d", len(s.Routes))
	}
}

func TestLoadState_NotExists(t *testing.T) {
	s := LoadState(filepath.Join(t.TempDir(), "nonexistent.json"))
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if len(s.Projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(s.Projects))
	}
}

func TestLoadState_Corrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte("not valid json {{{"), 0600)

	s := LoadState(path)
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if len(s.Projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(s.Projects))
	}

	// Corrupted file should be backed up
	if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
		t.Error("expected backup of corrupted file")
	}
}

func TestLoadState_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte(""), 0600)

	s := LoadState(path)
	if s == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestLoadState_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := NewState()
	original.Projects["myapp"] = &ProjectState{
		Containers: map[string]*ContainerState{
			"app": {
				ContainerID: "abc123def456",
				Image:       "nginx:latest",
				Status:      "running",
				HostPort:    8080,
			},
		},
	}
	SaveState(path, original)

	loaded := LoadState(path)
	if loaded == nil {
		t.Fatal("expected non-nil state")
	}
	if loaded.Version != StateVersion {
		t.Errorf("expected version %d, got %d", StateVersion, loaded.Version)
	}
	if _, ok := loaded.Projects["myapp"]; !ok {
		t.Error("expected myapp project")
	}
}

func TestLoadState_BackwardCompat_NoRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write old-format state without routes field
	data := []byte(`{"version":1,"projects":{"myapp":{"containers":{"app":{"container_id":"abc123","image":"nginx","status":"running"}}}}}`)
	os.WriteFile(path, data, 0600)

	s := LoadState(path)
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if len(s.Routes) != 0 {
		t.Errorf("expected empty routes for backward compat, got %d", len(s.Routes))
	}
	if _, ok := s.Projects["myapp"]; !ok {
		t.Error("expected myapp project")
	}
}

func TestSaveState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewState()
	if err := SaveState(path, s); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after save")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("state file should exist after save")
	}
}

func TestSaveState_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	path := filepath.Join(dir, "state.json")

	s := NewState()
	if err := SaveState(path, s); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("state file should exist after save")
	}
}

// Manager tests — containers

func TestManager_SetAndGetContainer(t *testing.T) {
	mgr := NewManager(t.TempDir())

	container := &ContainerState{
		ContainerID: "abc123def456",
		Image:       "nginx:latest",
		Status:      "running",
		HostPort:    8080,
	}

	if err := mgr.SetContainer("myapp", "app", container); err != nil {
		t.Fatalf("set container failed: %v", err)
	}

	state := mgr.GetState()
	if _, ok := state.Projects["myapp"]; !ok {
		t.Fatal("expected myapp project")
	}
	if c, ok := state.Projects["myapp"].Containers["app"]; !ok {
		t.Fatal("expected app container")
	} else {
		if c.ContainerID != "abc123def456" {
			t.Errorf("expected container ID abc123def456, got %q", c.ContainerID)
		}
		if c.HostPort != 8080 {
			t.Errorf("expected port 8080, got %d", c.HostPort)
		}
	}
}

func TestManager_UpdateStatus(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{
		ContainerID: "abc123",
		Image:       "nginx:latest",
		Status:      "running",
	})

	if err := mgr.UpdateStatus("myapp", "app", "stopped"); err != nil {
		t.Fatalf("update status failed: %v", err)
	}

	c := mgr.GetState().Projects["myapp"].Containers["app"]
	if c.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q", c.Status)
	}
}

func TestManager_RemoveContainer(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{ContainerID: "abc123"})
	mgr.SetContainer("myapp", "postgres", &ContainerState{ContainerID: "def456"})

	if err := mgr.RemoveContainer("myapp", "app"); err != nil {
		t.Fatalf("remove container failed: %v", err)
	}

	state := mgr.GetState()
	if _, ok := state.Projects["myapp"].Containers["app"]; ok {
		t.Error("app container should have been removed")
	}
	if _, ok := state.Projects["myapp"].Containers["postgres"]; !ok {
		t.Error("postgres container should still exist")
	}
}

func TestManager_RemoveContainer_LastOneDeletesProject(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{ContainerID: "abc123"})
	mgr.RemoveContainer("myapp", "app")

	if _, ok := mgr.GetState().Projects["myapp"]; ok {
		t.Error("empty project should have been removed")
	}
}

func TestManager_RemoveProject(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{ContainerID: "abc123"})
	mgr.SetContainer("myapp", "postgres", &ContainerState{ContainerID: "def456"})

	if err := mgr.RemoveProject("myapp"); err != nil {
		t.Fatalf("remove project failed: %v", err)
	}

	if _, ok := mgr.GetState().Projects["myapp"]; ok {
		t.Error("project should have been removed")
	}
}

func TestManager_RemoveProject_RemovesRoutes(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetRoute("yourplatform-myapp", "myapp", []string{"myapp.example.com"}, "127.0.0.1:3000")
	mgr.SetRoute("yourplatform-other", "other", []string{"other.example.com"}, "127.0.0.1:4000")

	if err := mgr.RemoveProject("myapp"); err != nil {
		t.Fatalf("remove project failed: %v", err)
	}

	routes := mgr.GetRoutes()
	if _, ok := routes["yourplatform-myapp"]; ok {
		t.Error("myapp route should have been removed")
	}
	if _, ok := routes["yourplatform-other"]; !ok {
		t.Error("other route should still exist")
	}
}

func TestManager_PersistsAcrossReloads(t *testing.T) {
	dir := t.TempDir()

	mgr1 := NewManager(dir)
	mgr1.SetContainer("myapp", "app", &ContainerState{
		ContainerID: "abc123",
		Image:       "nginx:latest",
		Status:      "running",
	})

	mgr2 := NewManager(dir)
	state := mgr2.GetState()

	c, ok := state.Projects["myapp"].Containers["app"]
	if !ok {
		t.Fatal("expected app container after reload")
	}
	if c.ContainerID != "abc123" {
		t.Errorf("expected container ID abc123, got %q", c.ContainerID)
	}
}

func TestManager_UpdateStatus_Nonexistent(t *testing.T) {
	mgr := NewManager(t.TempDir())
	if err := mgr.UpdateStatus("nonexistent", "app", "running"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_RemoveContainer_Nonexistent(t *testing.T) {
	mgr := NewManager(t.TempDir())
	if err := mgr.RemoveContainer("nonexistent", "app"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Manager tests — routes

func TestManager_SetRoute(t *testing.T) {
	mgr := NewManager(t.TempDir())

	if err := mgr.SetRoute("yourplatform-myshop", "myshop", []string{"myshop.example.com"}, "127.0.0.1:3000"); err != nil {
		t.Fatalf("set route failed: %v", err)
	}

	routes := mgr.GetRoutes()
	r, ok := routes["yourplatform-myshop"]
	if !ok {
		t.Fatal("expected route to exist")
	}
	if r.Project != "myshop" {
		t.Errorf("expected project 'myshop', got %q", r.Project)
	}
	if len(r.Domains) != 1 || r.Domains[0] != "myshop.example.com" {
		t.Errorf("expected domains [myshop.example.com], got %v", r.Domains)
	}
	if r.Upstream != "127.0.0.1:3000" {
		t.Errorf("expected upstream '127.0.0.1:3000', got %q", r.Upstream)
	}
}

func TestManager_SetRoute_Upsert(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetRoute("yourplatform-myshop", "myshop", []string{"myshop.example.com"}, "127.0.0.1:3000")
	mgr.SetRoute("yourplatform-myshop", "myshop", []string{"myshop.example.com", "custom.com"}, "127.0.0.1:4000")

	routes := mgr.GetRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route after upsert, got %d", len(routes))
	}
	r := routes["yourplatform-myshop"]
	if len(r.Domains) != 2 {
		t.Errorf("expected 2 domains after upsert, got %d", len(r.Domains))
	}
	if r.Upstream != "127.0.0.1:4000" {
		t.Errorf("expected upstream '127.0.0.1:4000', got %q", r.Upstream)
	}
}

func TestManager_RemoveRoute(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetRoute("yourplatform-myshop", "myshop", []string{"myshop.example.com"}, "127.0.0.1:3000")
	mgr.SetRoute("yourplatform-other", "other", []string{"other.example.com"}, "127.0.0.1:4000")

	if err := mgr.RemoveRoute("yourplatform-myshop"); err != nil {
		t.Fatalf("remove route failed: %v", err)
	}

	routes := mgr.GetRoutes()
	if _, ok := routes["yourplatform-myshop"]; ok {
		t.Error("myshop route should have been removed")
	}
	if _, ok := routes["yourplatform-other"]; !ok {
		t.Error("other route should still exist")
	}
}

func TestManager_RemoveRoute_Nonexistent(t *testing.T) {
	mgr := NewManager(t.TempDir())
	if err := mgr.RemoveRoute("nonexistent"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_GetRoutes_Empty(t *testing.T) {
	mgr := NewManager(t.TempDir())
	routes := mgr.GetRoutes()
	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestManager_Routes_PersistAcrossReloads(t *testing.T) {
	dir := t.TempDir()

	mgr1 := NewManager(dir)
	mgr1.SetRoute("yourplatform-app", "app", []string{"app.example.com"}, "127.0.0.1:3000")

	mgr2 := NewManager(dir)
	routes := mgr2.GetRoutes()
	if _, ok := routes["yourplatform-app"]; !ok {
		t.Error("expected route to persist across reloads")
	}
}

// ReconcileCaddy tests

type mockCaddyManager struct {
	routes map[string]caddy.CaddyRoute
}

func newMockCaddyManager() *mockCaddyManager {
	return &mockCaddyManager{routes: make(map[string]caddy.CaddyRoute)}
}

func (m *mockCaddyManager) SetRouteByID(routeID string, domains []string, upstream string) error {
	m.routes[routeID] = caddy.CaddyRoute{
		ID: routeID,
		Match: []caddy.CaddyMatch{
			{Host: domains},
		},
		Handle: []caddy.CaddyHandler{
			{
				Handler:   "reverse_proxy",
				Upstreams: []caddy.CaddyUpstream{{Dial: upstream}},
			},
		},
	}
	return nil
}

func (m *mockCaddyManager) DeleteRouteByID(routeID string) error {
	delete(m.routes, routeID)
	return nil
}

func (m *mockCaddyManager) GetRoutes() ([]caddy.CaddyRoute, error) {
	out := make([]caddy.CaddyRoute, 0, len(m.routes))
	for _, r := range m.routes {
		out = append(out, r)
	}
	return out, nil
}

func TestReconcileCaddy_RestoresRoutes(t *testing.T) {
	stateMgr := NewManager(t.TempDir())
	stateMgr.SetRoute("yourplatform-myshop", "myshop", []string{"myshop.example.com"}, "127.0.0.1:3000")

	caddyMock := newMockCaddyManager()

	restored, orphaned, err := ReconcileCaddy(context.Background(), stateMgr, caddyMock)
	if err != nil {
		t.Fatalf("ReconcileCaddy: %v", err)
	}
	if restored != 1 {
		t.Errorf("expected 1 restored, got %d", restored)
	}
	if orphaned != 0 {
		t.Errorf("expected 0 orphaned, got %d", orphaned)
	}
	if _, ok := caddyMock.routes["yourplatform-myshop"]; !ok {
		t.Error("route should be in Caddy after reconciliation")
	}
}

func TestReconcileCaddy_RemovesOrphanedRoutes(t *testing.T) {
	stateMgr := NewManager(t.TempDir())
	// State has no routes

	caddyMock := newMockCaddyManager()
	// Caddy has an orphaned route
	caddyMock.routes["yourplatform-old"] = caddy.CaddyRoute{
		ID: "yourplatform-old",
		Match: []caddy.CaddyMatch{
			{Host: []string{"old.example.com"}},
		},
	}

	restored, orphaned, err := ReconcileCaddy(context.Background(), stateMgr, caddyMock)
	if err != nil {
		t.Fatalf("ReconcileCaddy: %v", err)
	}
	if restored != 0 {
		t.Errorf("expected 0 restored, got %d", restored)
	}
	if orphaned != 1 {
		t.Errorf("expected 1 orphaned removed, got %d", orphaned)
	}
	if _, ok := caddyMock.routes["yourplatform-old"]; ok {
		t.Error("orphaned route should be removed from Caddy")
	}
}

func TestReconcileCaddy_PreservesExternalRoutes(t *testing.T) {
	stateMgr := NewManager(t.TempDir())
	// State has no routes

	caddyMock := newMockCaddyManager()
	// Caddy has a route without our prefix (externally managed)
	caddyMock.routes["external-route"] = caddy.CaddyRoute{
		ID: "external-route",
		Match: []caddy.CaddyMatch{
			{Host: []string{"external.example.com"}},
		},
	}

	_, orphaned, err := ReconcileCaddy(context.Background(), stateMgr, caddyMock)
	if err != nil {
		t.Fatalf("ReconcileCaddy: %v", err)
	}
	if orphaned != 0 {
		t.Errorf("expected 0 orphaned (external route preserved), got %d", orphaned)
	}
	if _, ok := caddyMock.routes["external-route"]; !ok {
		t.Error("external route should be preserved")
	}
}

func TestReconcileCaddy_Empty(t *testing.T) {
	stateMgr := NewManager(t.TempDir())
	caddyMock := newMockCaddyManager()

	restored, orphaned, err := ReconcileCaddy(context.Background(), stateMgr, caddyMock)
	if err != nil {
		t.Fatalf("ReconcileCaddy: %v", err)
	}
	if restored != 0 || orphaned != 0 {
		t.Errorf("expected 0/0, got %d/%d", restored, orphaned)
	}
}

// mockCaddyManagerError simulates a Caddy manager that fails on SetRouteByID.
type mockCaddyManagerError struct{}

func (m *mockCaddyManagerError) SetRouteByID(routeID string, domains []string, upstream string) error {
	return fmt.Errorf("caddy unavailable")
}

func (m *mockCaddyManagerError) DeleteRouteByID(routeID string) error {
	return nil
}

func (m *mockCaddyManagerError) GetRoutes() ([]caddy.CaddyRoute, error) {
	return nil, nil
}

func TestReconcileCaddy_PartialFailure(t *testing.T) {
	stateMgr := NewManager(t.TempDir())
	stateMgr.SetRoute("yourplatform-fail", "fail", []string{"fail.example.com"}, "127.0.0.1:3000")

	caddyMock := &mockCaddyManagerError{}

	restored, _, err := ReconcileCaddy(context.Background(), stateMgr, caddyMock)
	if err != nil {
		t.Fatalf("ReconcileCaddy should not fail: %v", err)
	}
	if restored != 0 {
		t.Errorf("expected 0 restored (all failed), got %d", restored)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(t.TempDir())
}

func newTestManagerWithDir(t *testing.T, path string) *Manager {
	t.Helper()
	return NewManager(filepath.Dir(path))
}

// --- Certificate State Tests ---

func TestSetCertificate(t *testing.T) {
	stateMgr := newTestManager(t)

	err := stateMgr.SetCertificate("example.com", "2026-09-01T00:00:00Z", "Let's Encrypt", "valid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cert := stateMgr.GetCertificate("example.com")
	if cert == nil {
		t.Fatal("expected certificate to be set")
	}
	if cert.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", cert.Domain)
	}
	if cert.Expiry != "2026-09-01T00:00:00Z" {
		t.Errorf("expected expiry 2026-09-01T00:00:00Z, got %s", cert.Expiry)
	}
	if cert.Issuer != "Let's Encrypt" {
		t.Errorf("expected issuer Let's Encrypt, got %s", cert.Issuer)
	}
	if cert.Status != "valid" {
		t.Errorf("expected status valid, got %s", cert.Status)
	}
}

func TestSetCertificate_Update(t *testing.T) {
	stateMgr := newTestManager(t)

	stateMgr.SetCertificate("example.com", "2026-09-01T00:00:00Z", "Let's Encrypt", "valid")
	stateMgr.SetCertificate("example.com", "2026-12-01T00:00:00Z", "Let's Encrypt", "expiring_soon")

	cert := stateMgr.GetCertificate("example.com")
	if cert == nil {
		t.Fatal("expected certificate to exist")
	}
	if cert.Expiry != "2026-12-01T00:00:00Z" {
		t.Errorf("expected updated expiry, got %s", cert.Expiry)
	}
	if cert.Status != "expiring_soon" {
		t.Errorf("expected updated status, got %s", cert.Status)
	}
}

func TestRemoveCertificate(t *testing.T) {
	stateMgr := newTestManager(t)

	stateMgr.SetCertificate("example.com", "2026-09-01T00:00:00Z", "Let's Encrypt", "valid")
	stateMgr.RemoveCertificate("example.com")

	cert := stateMgr.GetCertificate("example.com")
	if cert != nil {
		t.Errorf("expected certificate to be removed, got %+v", cert)
	}
}

func TestRemoveCertificate_Nonexistent(t *testing.T) {
	stateMgr := newTestManager(t)
	stateMgr.RemoveCertificate("nonexistent.com") // should not panic
}

func TestGetCertificates_Multiple(t *testing.T) {
	stateMgr := newTestManager(t)

	stateMgr.SetCertificate("a.com", "2026-09-01T00:00:00Z", "Let's Encrypt", "valid")
	stateMgr.SetCertificate("b.com", "2026-10-01T00:00:00Z", "Let's Encrypt", "valid")

	certs := stateMgr.GetCertificates()
	if len(certs) != 2 {
		t.Fatalf("expected 2 certificates, got %d", len(certs))
	}
	if _, ok := certs["a.com"]; !ok {
		t.Error("expected a.com in certificates")
	}
	if _, ok := certs["b.com"]; !ok {
		t.Error("expected b.com in certificates")
	}
}

func TestCertState_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Create state, add a cert
	stateMgr1 := NewManager(dir)
	stateMgr1.SetCertificate("persist.com", "2026-09-01T00:00:00Z", "Let's Encrypt", "valid")

	// Create new manager pointing at same directory — should reload from disk
	stateMgr2 := NewManager(dir)
	cert := stateMgr2.GetCertificate("persist.com")
	if cert == nil {
		t.Fatal("expected certificate to persist across manager instances")
	}
	if cert.Domain != "persist.com" {
		t.Errorf("expected domain persist.com, got %s", cert.Domain)
	}
}
