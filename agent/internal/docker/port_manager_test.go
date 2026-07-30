package docker

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Port mapping: app containers
// ---------------------------------------------------------------------------

func TestPortMapping_App(t *testing.T) {
	spec := &AppPortSpec{
		ContainerPort: 3000,
		HostPort:      0, // random high port
		BindAddress:   "127.0.0.1",
	}

	portMap, exposedPorts := PortMapping(ContainerTypeApp, spec)

	if len(portMap) == 0 {
		t.Fatal("expected non-empty port map for app container")
	}
	if len(exposedPorts) == 0 {
		t.Fatal("expected non-empty exposed ports for app container")
	}

	bindings, ok := portMap["3000/tcp"]
	if !ok {
		t.Fatal("expected port 3000/tcp in port map")
	}
	if len(bindings) == 0 {
		t.Fatal("expected at least one binding")
	}
	if bindings[0].HostIP != "127.0.0.1" {
		t.Errorf("expected bind to 127.0.0.1, got %s", bindings[0].HostIP)
	}
	if bindings[0].HostPort == "" || bindings[0].HostPort == "0" {
		t.Error("expected non-zero random high port")
	}
}

func TestPortMapping_App_NoSpec(t *testing.T) {
	portMap, exposedPorts := PortMapping(ContainerTypeApp, nil)
	if portMap != nil {
		t.Error("expected nil port map for nil spec")
	}
	if exposedPorts != nil {
		t.Error("expected nil exposed ports for nil spec")
	}
}

func TestPortMapping_App_SpecificPort(t *testing.T) {
	spec := &AppPortSpec{
		ContainerPort: 8080,
		HostPort:      18080,
		BindAddress:   "127.0.0.1",
	}

	portMap, _ := PortMapping(ContainerTypeApp, spec)

	bindings, ok := portMap["8080/tcp"]
	if !ok {
		t.Fatal("expected port 8080/tcp in port map")
	}
	if bindings[0].HostPort != "18080" {
		t.Errorf("expected host port 18080, got %s", bindings[0].HostPort)
	}
}

// ---------------------------------------------------------------------------
// Port mapping: database containers (no port exposure)
// ---------------------------------------------------------------------------

func TestPortMapping_Postgres(t *testing.T) {
	portMap, exposedPorts := PortMapping(ContainerTypePostgres, &AppPortSpec{ContainerPort: 5432})
	if portMap != nil {
		t.Error("expected nil port map for database container (no exposure)")
	}
	if exposedPorts != nil {
		t.Error("expected nil exposed ports for database container")
	}
}

func TestPortMapping_MySQL(t *testing.T) {
	portMap, exposedPorts := PortMapping(ContainerTypeMySQL, &AppPortSpec{ContainerPort: 3306})
	if portMap != nil {
		t.Error("expected nil port map for database container")
	}
	if exposedPorts != nil {
		t.Error("expected nil exposed ports for database container")
	}
}

func TestPortMapping_Redis(t *testing.T) {
	portMap, exposedPorts := PortMapping(ContainerTypeRedis, &AppPortSpec{ContainerPort: 6379})
	if portMap != nil {
		t.Error("expected nil port map for database container")
	}
	if exposedPorts != nil {
		t.Error("expected nil exposed ports for database container")
	}
}

// ---------------------------------------------------------------------------
// Database management port (optional explicit exposure)
// ---------------------------------------------------------------------------

func TestDBManagementPortMapping_Postgres(t *testing.T) {
	portMap, exposedPorts := DBManagementPortMapping(ContainerTypePostgres, 15432)
	if len(portMap) == 0 {
		t.Fatal("expected non-empty port map for DB management access")
	}
	if len(exposedPorts) == 0 {
		t.Fatal("expected non-empty exposed ports")
	}

	bindings, ok := portMap["5432/tcp"]
	if !ok {
		t.Fatal("expected port 5432/tcp")
	}
	if bindings[0].HostIP != "127.0.0.1" {
		t.Errorf("expected bind to 127.0.0.1, got %s", bindings[0].HostIP)
	}
	if bindings[0].HostPort != "15432" {
		t.Errorf("expected host port 15432, got %s", bindings[0].HostPort)
	}
}

func TestDBManagementPortMapping_Redis(t *testing.T) {
	portMap, _ := DBManagementPortMapping(ContainerTypeRedis, 16379)
	if portMap == nil {
		t.Fatal("expected port map for Redis management access")
	}
	bindings, ok := portMap["6379/tcp"]
	if !ok {
		t.Fatal("expected port 6379/tcp")
	}
	if bindings[0].HostPort != "16379" {
		t.Errorf("expected host port 16379, got %s", bindings[0].HostPort)
	}
}

func TestDBManagementPortMapping_Invalid(t *testing.T) {
	portMap, exposedPorts := DBManagementPortMapping("unknown", 0)
	if portMap != nil {
		t.Error("expected nil port map for invalid type")
	}
	if exposedPorts != nil {
		t.Error("expected nil exposed ports for invalid type")
	}
}

// ---------------------------------------------------------------------------
// Random high port range
// ---------------------------------------------------------------------------

func TestRandomHighPort_Range(t *testing.T) {
	for i := 0; i < 100; i++ {
		port := randomHighPort()
		if port < minHighPort || port > maxHighPort {
			t.Errorf("port %d outside valid range %d-%d", port, minHighPort, maxHighPort)
		}
	}
}

func TestRandomHighPort_Distribution(t *testing.T) {
	// Ensure we get different values (not always the same port)
	ports := make(map[int]bool)
	for i := 0; i < 50; i++ {
		port := randomHighPort()
		ports[port] = true
	}
	if len(ports) < 2 {
		t.Error("random high port should produce diverse values")
	}
}

// ---------------------------------------------------------------------------
// Database aliases
// ---------------------------------------------------------------------------

func TestDatabaseAliases_Postgres(t *testing.T) {
	aliases := DatabaseAliases(ContainerTypePostgres)
	if len(aliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(aliases))
	}
	if aliases[0] != "postgres" {
		t.Errorf("expected alias 'postgres', got '%s'", aliases[0])
	}
	if aliases[1] != "db" {
		t.Errorf("expected alias 'db', got '%s'", aliases[1])
	}
}

func TestDatabaseAliases_MySQL(t *testing.T) {
	aliases := DatabaseAliases(ContainerTypeMySQL)
	if len(aliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(aliases))
	}
	if aliases[0] != "mysql" {
		t.Errorf("expected alias 'mysql', got '%s'", aliases[0])
	}
	if aliases[1] != "db" {
		t.Errorf("expected alias 'db', got '%s'", aliases[1])
	}
}

func TestDatabaseAliases_Redis(t *testing.T) {
	aliases := DatabaseAliases(ContainerTypeRedis)
	if len(aliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(aliases))
	}
	if aliases[0] != "redis" {
		t.Errorf("expected alias 'redis', got '%s'", aliases[0])
	}
	if aliases[1] != "cache" {
		t.Errorf("expected alias 'cache', got '%s'", aliases[1])
	}
}

func TestDatabaseAliases_App(t *testing.T) {
	aliases := DatabaseAliases(ContainerTypeApp)
	if aliases != nil {
		t.Errorf("expected nil aliases for app container type, got %v", aliases)
	}
}

// ---------------------------------------------------------------------------
// DB default ports
// ---------------------------------------------------------------------------

func TestDbDefaultPort_Postgres(t *testing.T) {
	if got := dbDefaultPort(ContainerTypePostgres); got != 5432 {
		t.Errorf("expected 5432, got %d", got)
	}
}

func TestDbDefaultPort_MySQL(t *testing.T) {
	if got := dbDefaultPort(ContainerTypeMySQL); got != 3306 {
		t.Errorf("expected 3306, got %d", got)
	}
}

func TestDbDefaultPort_Redis(t *testing.T) {
	if got := dbDefaultPort(ContainerTypeRedis); got != 6379 {
		t.Errorf("expected 6379, got %d", got)
	}
}

func TestDbDefaultPort_App(t *testing.T) {
	if got := dbDefaultPort(ContainerTypeApp); got != 0 {
		t.Errorf("expected 0 for non-DB type, got %d", got)
	}
}
