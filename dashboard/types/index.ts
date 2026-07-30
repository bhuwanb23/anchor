export interface User {
  id: string
  email: string
}

export interface Server {
  id: string
  name: string
  status: "connected" | "disconnected" | "pending"
  connected_at: string
  last_seen: string
  // System info from preflight
  os_info?: string
  os_version?: string
  os_pretty?: string
  arch?: string
  ram_mb?: number
  ram_available_mb?: number
  disk_gb?: number
  disk_total_gb?: number
  disk_available_gb?: number
  disk_used_percent?: number
  docker_version?: string
  ip_address?: string
}

export interface ServerEvent {
  id: string
  server_id: string
  event_type: "warning" | "auto_fixed" | "alert"
  check_name?: string
  message?: string
  details?: string
  created_at: string
}

export interface Deployment {
  id: string
  server_id: string
  app_name: string
  image: string
  port: number
  domain: string
  status: "running" | "stopped" | "failed" | "deploying"
  created_at: string
  updated_at: string
}

export interface DeployRequest {
  app_name: string
  image: string
  port: number
  domain?: string
}

export interface LoginRequest {
  email: string
  password: string
}

export interface AuthResponse {
  token: string
}

export interface CreateServerRequest {
  name: string
}

export interface CreateServerResponse {
  id: string
  name: string
  token: string
  install_command: string
}

export interface RegistrationTokenResponse {
  token: string
  install_command: string
  expires_at: string
}
