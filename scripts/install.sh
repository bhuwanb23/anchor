#!/usr/bin/env bash
set -euo pipefail

# ================================================
# YourPlatform Agent Installer
# Usage: curl -fsSL https://get.yourplatform.com/install.sh | sudo bash -s -- --token=reg_...
# ================================================

TOKEN=""
BASE_URL="${BASE_URL:-https://get.yourplatform.com}"
AGENT_VERSION="${AGENT_VERSION:-0.1.0}"

# ─────────────────────────────────────────────
# Argument Parsing
# ─────────────────────────────────────────────

for arg in "$@"; do
  case $arg in
    --token=*)    TOKEN="${arg#*=}" ;;
    --token)      shift; TOKEN="${1:-}" ;;
    --base-url=*) BASE_URL="${arg#*=}" ;;
    --base-url)   shift; BASE_URL="${1:-}" ;;
  esac
done

if [ -z "$TOKEN" ]; then
  echo "Error: --token is required"
  echo ""
  echo "Usage:"
  echo "  curl -fsSL ${BASE_URL}/install.sh | sudo bash -s -- --token=reg_..."
  echo ""
  echo "Get a token from your dashboard at ${BASE_URL}/servers"
  exit 1
fi

if [[ ! "$TOKEN" =~ ^reg_ ]]; then
  echo "Error: token must start with reg_"
  echo "Got: ${TOKEN:0:20}..."
  echo ""
  echo "Get a valid token from your dashboard at ${BASE_URL}/servers"
  exit 1
fi

# ─────────────────────────────────────────────
# Root Check
# ─────────────────────────────────────────────

if [ "$(id -u)" -ne 0 ]; then
  echo "Error: this script must be run as root"
  echo ""
  echo "Run: sudo bash $0 --token=${TOKEN}"
  exit 1
fi

# ─────────────────────────────────────────────
# OS Detection
# ─────────────────────────────────────────────

OS_ID=""
OS_VERSION=""

detect_os() {
  if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS_ID="${ID}"
    OS_VERSION="${VERSION_ID}"
  else
    echo "Error: cannot detect OS — /etc/os-release not found"
    exit 1
  fi

  case "$OS_ID" in
    ubuntu|debian|centos|rhel|amzn|fedora) ;;
    *)
      echo "Error: unsupported OS '${OS_ID}'"
      echo "Supported: ubuntu, debian, centos, rhel, amzn, fedora"
      exit 1
      ;;
  esac
}

# ─────────────────────────────────────────────
# Architecture Detection
# ─────────────────────────────────────────────

ARCH=""

detect_arch() {
  local arch_raw
  arch_raw=$(uname -m)
  case "$arch_raw" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)
      echo "Error: unsupported architecture '${arch_raw}'"
      echo "Supported: x86_64 (amd64), aarch64 (arm64)"
      exit 1
      ;;
  esac
}

# ─────────────────────────────────────────────
# Dependency Check
# ─────────────────────────────────────────────

check_deps() {
  local missing=()

  if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
    missing+=("curl or wget")
  fi

  if ! command -v systemctl &>/dev/null; then
    missing+=("systemd (systemctl)")
  fi

  if ! command -v sha256sum &>/dev/null && ! command -v shasum &>/dev/null; then
    missing+=("sha256sum or shasum")
  fi

  if [ ${#missing[@]} -gt 0 ]; then
    echo "Error: missing dependencies: ${missing[*]}"
    echo ""
    echo "Install them with:"
    echo "  apt-get update && apt-get install -y curl coreutils"
    exit 1
  fi
}

# ─────────────────────────────────────────────
# Binary Download
# ─────────────────────────────────────────────

TMPDIR=""

download() {
  local download_url="${BASE_URL}/releases/v${AGENT_VERSION}/yourplatform-agent-${OS_ID}-${ARCH}"
  local checksum_url="${download_url}.sha256"

  TMPDIR=$(mktemp -d)
  local binary_path="${TMPDIR}/yourplatform-agent"

  echo "Downloading agent v${AGENT_VERSION} for ${OS_ID} ${ARCH}..."

  if command -v curl &>/dev/null; then
    curl -fsSL -o "$binary_path" "$download_url"
    curl -fsSL -o "${binary_path}.sha256" "$checksum_url"
  else
    wget -qO "$binary_path" "$download_url"
    wget -qO "${binary_path}.sha256" "$checksum_url"
  fi

  echo "Download complete."
}

# ─────────────────────────────────────────────
# Checksum Verification
# ─────────────────────────────────────────────

verify_checksum() {
  local binary_path="${TMPDIR}/yourplatform-agent"
  local checksum_path="${binary_path}.sha256"

  echo "Verifying checksum..."

  local expected
  expected=$(awk '{print $1}' "$checksum_path")

  local actual
  if command -v sha256sum &>/dev/null; then
    actual=$(sha256sum "$binary_path" | awk '{print $1}')
  else
    actual=$(shasum -a 256 "$binary_path" | awk '{print $1}')
  fi

  if [ "$expected" != "$actual" ]; then
    echo ""
    echo "Error: checksum mismatch!"
    echo "Expected: $expected"
    echo "Got:      $actual"
    echo ""
    echo "The downloaded binary may be corrupted."
    echo "Try again or contact support."
    rm -rf "$TMPDIR"
    exit 1
  fi

  echo "Checksum verified."
}

# ─────────────────────────────────────────────
# Binary Installation
# ─────────────────────────────────────────────

install_binary() {
  local binary_path="${TMPDIR}/yourplatform-agent"

  chmod +x "$binary_path"
  mv "$binary_path" /usr/local/bin/yourplatform-agent

  mkdir -p /etc/yourplatform
  mkdir -p /var/lib/yourplatform

  cat > /etc/yourplatform/config.yaml <<EOF
control_plane_url: ${BASE_URL}
agent_token: ${TOKEN}
server_id: ""
docker_socket: unix:///var/run/docker.sock
caddy_config_dir: /etc/caddy
backup_dest: ""
ws_reconnect_sec: 5
log_level: info
EOF

  echo "Binary installed to /usr/local/bin/yourplatform-agent"
  echo "Config written to /etc/yourplatform/config.yaml"
}

# ─────────────────────────────────────────────
# Pre-flight Check
# ─────────────────────────────────────────────

run_preflight() {
  echo ""
  echo "Running preflight checks..."

  local output
  output=$(yourplatform-agent preflight 2>&1) || true
  echo "$output"

  if echo "$output" | grep -qE "status=missing|status=error"; then
    echo ""
    echo "Preflight checks failed. Fix the issues above and try again."
    rm -rf "$TMPDIR"
    exit 1
  fi

  echo "Preflight checks passed."
}

# ─────────────────────────────────────────────
# Systemd Service
# ─────────────────────────────────────────────

install_service() {
  cat > /etc/systemd/system/yourplatform-agent.service <<EOF
[Unit]
Description=YourPlatform Agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/yourplatform-agent run
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=yourplatform-agent

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable yourplatform-agent
  systemctl start yourplatform-agent

  echo "Service created and started."
}

# ─────────────────────────────────────────────
# Wait for Connection
# ─────────────────────────────────────────────

wait_for_connection() {
  echo ""
  echo "Waiting for agent to connect to control plane..."

  local connected_file="/var/lib/yourplatform/agent.connected"
  local timeout=60
  local elapsed=0

  while [ "$elapsed" -lt "$timeout" ]; do
    if [ -f "$connected_file" ]; then
      echo "Agent connected successfully!"
      return 0
    fi

    if ! systemctl is-active --quiet yourplatform-agent; then
      echo ""
      echo "Error: agent service is not running."
      echo ""
      echo "Last 20 lines of logs:"
      journalctl -u yourplatform-agent -n 20 --no-pager
      echo ""
      echo "Troubleshooting:"
      echo "  1. Check logs:       journalctl -u yourplatform-agent -f"
      echo "  2. Check config:     cat /etc/yourplatform/config.yaml"
      echo "  3. Verify firewall:  allow outbound to ${BASE_URL}"
      rm -rf "$TMPDIR"
      exit 1
    fi

    sleep 2
    elapsed=$((elapsed + 2))
  done

  echo ""
  echo "Warning: agent did not connect within ${timeout}s."
  echo "The service is running but may not have reached the control plane."
  echo ""
  echo "Possible causes:"
  echo "  1. Firewall blocking outbound to ${BASE_URL}"
  echo "  2. DNS cannot resolve $(echo "$BASE_URL" | sed 's|https\?://||;s|/.*||')"
  echo "  3. Control plane not reachable at ${BASE_URL}"
  echo ""
  echo "Debug commands:"
  echo "  systemctl status yourplatform-agent"
  echo "  journalctl -u yourplatform-agent -f"
  echo "  curl -v ${BASE_URL}/health"
  rm -rf "$TMPDIR"
  exit 1
}

# ─────────────────────────────────────────────
# Cleanup
# ─────────────────────────────────────────────

cleanup() {
  if [ -n "$TMPDIR" ] && [ -d "$TMPDIR" ]; then
    rm -rf "$TMPDIR"
  fi
}

# ─────────────────────────────────────────────
# Success
# ─────────────────────────────────────────────

print_success() {
  echo ""
  echo "========================================"
  echo " YourPlatform Agent Installed!"
  echo "========================================"
  echo ""
  echo "Your server is being connected to the control plane."
  echo "Return to the dashboard to manage it."
  echo ""
  echo "Useful commands:"
  echo "  systemctl status yourplatform-agent"
  echo "  journalctl -u yourplatform-agent -f"
}

# ─────────────────────────────────────────────
# Main
# ─────────────────────────────────────────────

trap cleanup EXIT

echo "YourPlatform Agent Installer v${AGENT_VERSION}"
echo "────────────────────────────────────────"

detect_os
detect_arch
check_deps
download
verify_checksum
install_binary
run_preflight
install_service
wait_for_connection
print_success
