# Install Script

The `install.sh` script provisions a server with the YourPlatform agent.

## Usage

```bash
# From a remote server (after CDN setup)
curl -fsSL https://get.yourplatform.com/install.sh | sudo bash -s -- --token=reg_...

# From local control plane (development)
curl -fsSL http://localhost:8080/install.sh | sudo bash -s -- --token=reg_... --base-url=http://localhost:8080

# Direct execution
sudo bash scripts/install.sh --token=reg_... --base-url=http://localhost:8080
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BASE_URL` | `https://get.yourplatform.com` | Control plane URL |
| `AGENT_VERSION` | `0.1.0` | Agent version to install |

## What It Does

1. Validates the `--token` argument (must start with `reg_`)
2. Checks root privileges
3. Detects OS (ubuntu/debian/centos/rhel/amzn/fedora) and architecture (amd64/arm64)
4. Checks dependencies (curl/wget, systemd, sha256sum)
5. Downloads the agent binary for the detected platform
6. Verifies SHA-256 checksum
7. Installs binary to `/usr/local/bin/yourplatform-agent`
8. Writes config to `/etc/yourplatform/config.yaml`
9. Runs agent preflight checks
10. Creates and starts systemd service
11. Waits for agent to connect to control plane

## Testing

### Docker (Ubuntu)

```bash
# Build a test container
docker run -it --rm \
  -v $(pwd)/scripts/install.sh:/tmp/install.sh \
  -v $(pwd)/bin/yourplatform-agent-linux-amd64:/tmp/yourplatform-agent \
  ubuntu:22.04 bash

# Inside the container, simulate the download by placing the binary
# Then run the script with a test token
```

### Direct Testing

```bash
# Start the control plane
make dev-backend

# In another terminal, run the script
sudo bash scripts/install.sh \
  --token=reg_test_token \
  --base-url=http://localhost:8080
```

## File Locations After Install

| Path | Description |
|------|-------------|
| `/usr/local/bin/yourplatform-agent` | Agent binary |
| `/etc/yourplatform/config.yaml` | Agent configuration |
| `/var/lib/yourplatform/` | Agent data directory |
| `/etc/systemd/system/yourplatform-agent.service` | Systemd service unit |
