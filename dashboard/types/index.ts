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

export interface LogEntry {
  type: "log_line"
  project: string
  container: string
  stream: "stdout" | "stderr"
  line: string
  timestamp: string
}

export interface LogHistory {
  type: "log_history"
  project: string
  container: string
  lines: LogEntry[]
}

export interface StreamLogsCommand {
  type: "stream_logs"
  project_name: string
  containers: string[]
  tail?: number
}

export interface StopStreamLogsCommand {
  type: "stop_stream_logs"
  container_id?: string
  all?: boolean
}

// Backup types
export interface BackupComponentResult {
  type: string
  name: string
  size_bytes: number
  status: "success" | "failed"
}

export interface BackupProjectResult {
  name: string
  status: "success" | "partial" | "failed"
  components?: BackupComponentResult[]
  error?: string
}

export interface BackupJob {
  id: string
  server_id: string
  status: "pending" | "running" | "success" | "partial" | "failed"
  started_at?: string
  completed_at?: string
  error_message?: string
  snapshot_id?: string
  duration_seconds?: number
  size_new_bytes?: number
  size_total_bytes?: number
  project_results?: string // JSON string of BackupProjectResult[]
  retention_applied?: boolean
  snapshots_pruned?: number
  created_at: string
}

export interface BackupSchedule {
  id: string
  server_id: string
  enabled: boolean
  schedule: string
  retention_daily: number
  retention_weekly: number
  retention_monthly: number
  hour_utc?: number
  last_backup_at?: string
  next_backup_at?: string
  created_at: string
  updated_at: string
}

export interface BackupUsage {
  total_bytes: number
  snapshot_count: number
}
