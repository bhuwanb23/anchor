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
