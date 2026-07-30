package docker

import (
	"fmt"
	"math/rand"
	"net"

	"github.com/docker/go-connections/nat"
)

// Container types for port exposure decisions.
const (
	// Minimum and maximum for the random high port range.
	// We use the IANA dynamic/private port range (49152–65535).
	minHighPort = 49152
	maxHighPort = 65535
)

// AppPortSpec describes how an application's port should be exposed.
type AppPortSpec struct {
	ContainerPort int    // Port the app listens on inside the container (e.g., 3000, 80)
	HostPort      int    // Port on the host (0 = random high port)
	BindAddress   string // Host bind address (defaults to "127.0.0.1")
}

// PortMapping converts an AppPortSpec into Docker port bindings and exposed ports.
// For app containers: port is exposed on 127.0.0.1:random-high-port.
// For database containers: no port is exposed (return empty mappings).
func PortMapping(ct ContainerType, spec *AppPortSpec) (nat.PortMap, nat.PortSet) {
	switch ct {
	case ContainerTypeApp:
		return appPortMapping(spec)
	case ContainerTypePostgres, ContainerTypeMySQL, ContainerTypeRedis:
		// Database containers: no port exposure to host by default.
		// They are only reachable from within the project network.
		return nil, nil
	default:
		return appPortMapping(spec)
	}
}

// appPortMapping creates port bindings for an application container.
// The port is bound to 127.0.0.1 (not 0.0.0.0) so that traffic must
// go through Caddy — users cannot reach the app directly.
func appPortMapping(spec *AppPortSpec) (nat.PortMap, nat.PortSet) {
	if spec == nil || spec.ContainerPort == 0 {
		return nil, nil
	}

	bindAddr := spec.BindAddress
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	hostPort := spec.HostPort
	if hostPort == 0 {
		hostPort = randomHighPort()
	}

	containerPort := spec.ContainerPort

	portProto := nat.Port(fmt.Sprintf("%d/tcp", containerPort))
	portMap := nat.PortMap{
		portProto: []nat.PortBinding{
			{
				HostIP:   bindAddr,
				HostPort: fmt.Sprintf("%d", hostPort),
			},
		},
	}
	exposedPorts := nat.PortSet{
		portProto: struct{}{},
	}

	return portMap, exposedPorts
}

// randomHighPort picks a random port in the dynamic/private range.
func randomHighPort() int {
	return minHighPort + rand.Intn(maxHighPort-minHighPort+1)
}

// AvailablePort checks if a given TCP port is available on the host.
// Used when a specific port was requested but we need to verify it's free.
func AvailablePort(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// DBManagementPortMapping creates port bindings for a database container
// when the user has explicitly enabled database management access.
// The port is bound to 127.0.0.1 only — still requires an SSH tunnel
// to reach from the user's laptop.
func DBManagementPortMapping(dbType ContainerType, hostPort int) (nat.PortMap, nat.PortSet) {
	containerPort := dbDefaultPort(dbType)
	if containerPort == 0 || hostPort == 0 {
		return nil, nil
	}

	portProto := nat.Port(fmt.Sprintf("%d/tcp", containerPort))
	portMap := nat.PortMap{
		portProto: []nat.PortBinding{
			{
				HostIP:   "127.0.0.1",
				HostPort: fmt.Sprintf("%d", hostPort),
			},
		},
	}
	exposedPorts := nat.PortSet{
		portProto: struct{}{},
	}

	return portMap, exposedPorts
}

// dbDefaultPort returns the default internal port for a database container type.
func dbDefaultPort(dbType ContainerType) int {
	switch dbType {
	case ContainerTypePostgres:
		return 5432
	case ContainerTypeMySQL:
		return 3306
	case ContainerTypeRedis:
		return 6379
	default:
		return 0
	}
}
