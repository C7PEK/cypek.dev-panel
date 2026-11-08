export type Role = "owner" | "admin" | "user"

export interface User {
  id: number
  username: string
  role: Role
  disabled: boolean
  totpEnabled: boolean
  createdAt: string
  lastLoginAt?: string
}

export interface SessionResponse {
  user: User
  csrfToken: string
}

export interface SetupStatus {
  setupRequired: boolean
}

export interface SystemInfo {
  version: string
  hostname: string
  os: string
  arch: string
  goVersion: string
  dev: boolean
  addresses: { ip: string; interface: string; kind: "lan" | "tailscale" | "other" }[]
  urls: string[]
}

// --- Servers -----------------------------------------------------------------

export type ServerState = "stopped" | "starting" | "running" | "stopping" | "crashed" | "unknown"

export type ServerCategory = "fivem" | "minecraft" | "discord-bot" | "other"

export interface ServerStatus {
  state: ServerState
  pid?: number
  since?: string
  exitCode?: number
  /** Why the server crashed, when the shim recorded a reason. */
  message?: string
  memoryBytes?: number
  cpuNanos?: number
}

export interface RunConfig {
  command: string
  args: string[]
  env?: Record<string, string>
  stop: { command?: string; timeoutSec?: number }
  readyRegex?: string
}

export interface Port {
  name: string
  port: number
  protocol: "tcp" | "udp" | "tcp+udp"
}

/** 0 means unlimited. */
export interface Limits {
  memoryMb: number
  cpuPercent: number
}

export interface Server {
  id: string
  name: string
  templateId: string
  category: ServerCategory
  run: RunConfig
  variables: Record<string, string>
  ports: Port[]
  autostart: boolean
  limits: Limits
  createdBy?: number
  createdAt: string
  updatedAt: string
  status: ServerStatus
}

export interface CreateServerInput {
  name: string
  run: RunConfig
  limits?: Limits
  autostart?: boolean
  ports?: Port[]
}

export type UpdateServerInput = Partial<Pick<Server, "name" | "run" | "limits" | "autostart" | "ports">>

export type PowerAction = "start" | "stop" | "restart" | "kill"

// --- Console WebSocket frames -------------------------------------------------

export type ConsoleServerFrame =
  | { type: "hello"; state: ServerState; pid?: number; startedAt?: string }
  | { type: "output"; data: string }
  | { type: "state"; state: ServerState; exitCode?: number; message?: string }
  | { type: "error"; message: string }

export type ConsoleClientFrame = { type: "input"; data: string } | { type: "resize"; cols: number; rows: number }
