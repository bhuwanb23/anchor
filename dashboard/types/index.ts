// ============================================================================
// YourPlatform — Centralized Type Definitions
// Single source of truth for all API data contracts.
// ============================================================================

// ---------------------------------------------------------------------------
// Status Union Types
// ---------------------------------------------------------------------------

export type ServerStatus = "pending" | "connected" | "disconnected" | "updating" | "error";
export type AppStatus = "deploying" | "running" | "stopped" | "failed" | "crashed" | "removing";
export type DeploymentStatus = "deploying" | "running" | "stopped" | "failed";
export type CommandStatus = "queued" | "in_progress" | "success" | "failed" | "timeout";
export type AlertSeverity = "warning" | "critical";
export type AlertStatus = "active" | "resolved" | "acknowledged";
export type BackupJobStatus = "pending" | "running" | "success" | "partial" | "failed";
export type BackupVerificationStatus = "verified" | "failed" | "skipped" | "";
export type RestoreJobStatus = "pending" | "running" | "success" | "partial" | "failed";
export type DBType = "postgres" | "mysql" | "redis";
export type ProjectDatabaseStatus = "running" | "stopped" | "failed";
export type CustomDomainStatus = "pending" | "active" | "failed";
export type LogStream = "stdout" | "stderr";
export type TeamRole = "owner" | "admin" | "member";
export type RemediationAction = "docker_prune" | "caddy_restart" | "crash_recovery" | "memory_flush";

// ---------------------------------------------------------------------------
// User
// ---------------------------------------------------------------------------

export interface User {
  id: string;
  email: string;
  name: string;
  created_at?: string;
}

export interface AuthUser {
  id: string;
  email: string;
  name: string;
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

export interface LoginRequest {
  email: string;
  password: string;
}

export interface AuthResponse {
  access_token: string;
  refresh_token: string;
  token_type: "Bearer";
  expires_in: number;
  user: AuthUser;
}

export interface RegisterRequest {
  name: string;
  email: string;
  password: string;
}

export interface RegisterResponse {
  message: string;
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

export interface Server {
  id: string;
  name: string;
  status: ServerStatus;
  public_ip?: string;
  os?: string;
  os_version?: string;
  arch?: string;
  cpu_count?: number;
  ram_total_mb?: number;
  disk_total_gb?: number;
  connected_at?: string;
  last_seen?: string;
  created_at?: string;
  agent_version?: string;
  metrics?: MetricsSnapshot;
}

export interface MetricsSnapshot {
  cpu_percent: number;
  ram_used_mb: number;
  ram_total_mb: number;
  ram_percent: number;
  disk_used_gb: number;
  disk_total_gb: number;
  disk_percent: number;
  load_1min: number;
}

// ---------------------------------------------------------------------------
// Server Events
// ---------------------------------------------------------------------------

export interface ServerEvent {
  id: string;
  server_id: string;
  event_type: string;
  check_name?: string;
  message?: string;
  details?: string;
  created_at?: string;
}

export interface RemediationReport {
  type: "remediation_report";
  payload: {
    server_id: string;
    action: RemediationAction;
    success: boolean;
    message: string;
    project?: string;
    container?: string;
    freed_bytes?: number;
    disk_percent_before?: number;
    disk_percent_after?: number;
    at: string;
  };
}

// ---------------------------------------------------------------------------
// App
// ---------------------------------------------------------------------------

export interface App {
  id: string;
  server_id: string;
  project_name: string;
  status: AppStatus;
  current_image?: string;
  current_host_port?: number;
  platform_domain?: string;
  memory_limit_mb: number;
  cpu_quota_percent: number;
  app_port: number;
  created_at?: string;
  updated_at?: string;
}

// ---------------------------------------------------------------------------
// Deployment
// ---------------------------------------------------------------------------

export interface Deployment {
  id: string;
  server_id: string;
  app_name: string;
  image: string;
  port: number;
  domain?: string;
  status: DeploymentStatus;
  created_at?: string;
  updated_at?: string;
}

export interface DeployRequest {
  image: string;
  port?: number;
}

// ---------------------------------------------------------------------------
// Command
// ---------------------------------------------------------------------------

export interface Command {
  id: string;
  server_id: string;
  command_type: string;
  project_key: string;
  payload: string;
  status: CommandStatus;
  issued_by: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  result?: string;
}

// ---------------------------------------------------------------------------
// Env Var
// ---------------------------------------------------------------------------

export interface EnvVarKey {
  id: string;
  key_name: string;
  is_auto: boolean;
  created_at?: string;
  updated_at?: string;
}

// ---------------------------------------------------------------------------
// Alert
// ---------------------------------------------------------------------------

export interface Alert {
  id: string;
  server_id: string;
  server_name?: string;
  project?: string;
  container?: string;
  severity: AlertSeverity;
  level?: AlertSeverity | "resolved";
  type: string;
  status: AlertStatus;
  title?: string;
  message: string;
  detail?: string;
  action?: string;
  fired_at: string;
  resolved_at?: string | null;
  read_at?: string | null;
  acknowledged_at?: string | null;
  acknowledged_by?: string | null;
  created_at?: string;
  metrics?: Record<string, number | string | boolean>;
}

export interface AnomalyAlert {
  level: AlertSeverity | "resolved";
  type: string;
  project?: string;
  container?: string;
  message: string;
}

// ---------------------------------------------------------------------------
// Container State (from agent via WebSocket)
// ---------------------------------------------------------------------------

export interface ContainerState {
  project: string;
  role: "app" | "postgres" | "redis";
  status: string;
  health?: string;
  cpu_percent?: number;
  ram_used_mb?: number;
  ram_limit_mb?: number;
  restart_count?: number;
}

// ---------------------------------------------------------------------------
// Backup
// ---------------------------------------------------------------------------

export interface BackupComponentResult {
  type: string;
  name: string;
  size_bytes: number;
  status: "success" | "failed";
}

export interface BackupProjectResult {
  name: string;
  status: "success" | "partial" | "failed";
  components?: BackupComponentResult[];
  error?: string;
}

export interface BackupJob {
  id: string;
  server_id: string;
  status: BackupJobStatus;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
  snapshot_id?: string;
  duration_seconds?: number;
  size_new_bytes?: number;
  size_total_bytes?: number;
  project_results?: string;
  retention_applied?: boolean;
  snapshots_pruned?: number;
  created_at: string;
  verification_status?: BackupVerificationStatus;
  verification_error?: string;
}

export interface BackupSchedule {
  id: string;
  server_id: string;
  enabled: boolean;
  schedule: string;
  retention_daily: number;
  retention_weekly: number;
  retention_monthly: number;
  hour_utc?: number;
  last_backup_at?: string;
  next_backup_at?: string;
  created_at: string;
  updated_at: string;
}

export interface BackupUsageHistoryPoint {
  size_bytes: number;
  recorded_at: string;
}

export interface BackupUsage {
  total_bytes: number;
  snapshot_count: number;
  limit_bytes: number;
  percent_used: number;
  history: BackupUsageHistoryPoint[];
}

export interface StorageStats {
  total_bytes: number;
  limit_bytes: number;
  percent_used: number;
  snapshot_count: number;
  days_until_full: number;
  retention_daily: number;
  retention_weekly: number;
  retention_monthly: number;
  history: BackupUsageHistoryPoint[];
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

export interface RestoreRequest {
  snapshot_id: string;
  project_name: string;
}

export interface RestoreJob {
  id: string;
  server_id: string;
  status: RestoreJobStatus;
  snapshot_id?: string;
  project_name?: string;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
  duration_seconds?: number;
  created_at: string;
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

export interface VerificationSchedule {
  config: {
    id: string;
    server_id: string;
    last_verification_at?: string;
    next_verification_at?: string;
    last_full_verification_at?: string;
    next_full_verification_at?: string;
    verify_interval_hours: number;
    full_verify_interval_hours: number;
    created_at: string;
    updated_at: string;
  };
  last_verification: {
    status: string;
    error?: string;
  };
}

// ---------------------------------------------------------------------------
// Log Types
// ---------------------------------------------------------------------------

export interface LogEntry {
  type: "log_line";
  project: string;
  container: string;
  stream: LogStream;
  line: string;
  timestamp: string;
}

export interface LogHistory {
  type: "log_history";
  project: string;
  container: string;
  lines: LogEntry[];
}

export interface LogLines {
  type: "log_lines";
  project: string;
  container: string;
  lines: LogEntry[];
}

export interface StreamEnded {
  type: "stream_ended";
  project: string;
  container: string;
  reason: "container_stopped" | "read_error";
}

// ---------------------------------------------------------------------------
// WebSocket Command Messages (browser → agent)
// ---------------------------------------------------------------------------

export interface StreamLogsCommand {
  type: "stream_logs";
  project_name: string;
  containers: string[];
  tail?: number;
}

export interface StopStreamLogsCommand {
  type: "stop_stream_logs";
  container_id?: string;
  all?: boolean;
}

export type WSBrowserCommand = StreamLogsCommand | StopStreamLogsCommand;

// ---------------------------------------------------------------------------
// WebSocket Message Types (agent → browser, server → browser)
// ---------------------------------------------------------------------------

export interface WSMessageBase {
  type: string;
  server_id?: string;
  payload?: unknown;
}

export interface WSServerUpdateMessage extends WSMessageBase {
  type: "server_update";
  payload: {
    status?: ServerStatus;
    metrics?: MetricsSnapshot;
    containers?: ContainerState[];
  };
}

export interface WSCommandProgressMessage extends WSMessageBase {
  type: "command_progress";
  payload: {
    command_id: string;
    status: CommandStatus;
    message?: string;
  };
}

export interface WSCommandResultMessage extends WSMessageBase {
  type: "command_result";
  payload: {
    command_id: string;
    status: "success" | "failed" | "timeout";
    result?: string;
    error?: string;
  };
}

export interface WSLogHistoryMessage extends WSMessageBase {
  type: "initial_logs" | "log_history";
  payload: LogHistory;
}

export interface WSLogLinesMessage extends WSMessageBase {
  type: "log_lines";
  payload: LogLines;
}

export interface WSStreamEndedMessage extends WSMessageBase {
  type: "stream_ended";
  payload: StreamEnded;
}

export interface WSAlertMessage extends WSMessageBase {
  type: "anomaly_alert" | "error_alert";
  payload: Partial<Alert>;
}

export interface WSAgentConnectedMessage extends WSMessageBase {
  type: "agent_connected";
  payload: {
    server_id: string;
    agent_id?: string;
  };
}

export interface WSAgentDisconnectedMessage extends WSMessageBase {
  type: "agent_disconnected";
  payload: {
    server_id: string;
  };
}

export type WSIncomingMessage =
  | WSServerUpdateMessage
  | WSCommandProgressMessage
  | WSCommandResultMessage
  | WSLogHistoryMessage
  | WSLogLinesMessage
  | WSStreamEndedMessage
  | WSAlertMessage
  | WSAgentConnectedMessage
  | WSAgentDisconnectedMessage
  | WSMessageBase;

// ---------------------------------------------------------------------------
// Request Types
// ---------------------------------------------------------------------------

export interface CreateServerRequest {
  name: string;
}

export interface CreateServerResponse {
  id: string;
  name: string;
  token: string;
  install_command: string;
}

export interface RegistrationTokenResponse {
  token: string;
  install_command: string;
  expires_at: string;
}

export interface InviteMemberRequest {
  email: string;
  role: TeamRole;
}

export interface UpdateEnvVarRequest {
  value: string;
}

// ---------------------------------------------------------------------------
// API List Wrapper
// ---------------------------------------------------------------------------

export interface ListResponse<T> {
  data: T[];
  total?: number;
}
