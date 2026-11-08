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

export type InstallState = "pending" | "installing" | "ready" | "failed"

export interface Server {
  id: string
  name: string
  /** "custom" for command servers, otherwise a template id. */
  templateId: string
  category: ServerCategory
  run: RunConfig
  /** User variables only; secrets are masked as "••••••••". */
  variables: Record<string, string>
  ports: Port[]
  autostart: boolean
  limits: Limits
  installState: InstallState
  installError?: string
  createdBy?: number
  createdAt: string
  updatedAt: string
  status: ServerStatus
}

export interface CreateServerInput {
  name: string
  /** Custom servers. */
  run?: RunConfig
  /** Template servers: all values as strings ("true"/"false", "2048"). */
  templateId?: string
  variables?: Record<string, string>
  limits?: Limits
  autostart?: boolean
  ports?: Port[]
}

export type UpdateServerInput = Partial<Pick<Server, "name" | "run" | "limits" | "autostart" | "ports" | "variables">>

// --- Templates -----------------------------------------------------------------

export type LocalizedText = Record<string, string>

export type VariableType = "string" | "number" | "bool" | "select" | "secret"

export interface TemplateVariable {
  key: string
  label: LocalizedText
  help?: LocalizedText
  type: VariableType
  default?: string
  options?: string[]
  source?: string
  min?: number
  max?: number
  maxLength?: number
  required?: boolean
  /** A bool the user must switch on (EULA, license terms). */
  consent?: boolean
}

export interface TemplatePort {
  name: string
  /** May contain a placeholder such as "{{PORT}}". */
  port: string
  protocol: string
}

export interface Template {
  id: string
  name: string
  category: ServerCategory
  icon?: string
  description?: LocalizedText
  variables: TemplateVariable[]
  runtime: { kind: string; version?: string }
  ports?: TemplatePort[]
  builtin: boolean
}

export interface SourceOption {
  value: string
  label: string
  prerelease?: boolean
}

export type InstallJobState = "running" | "done" | "failed"

export interface InstallJob {
  serverId: string
  state: InstallJobState
  step: number
  steps: number
  stepTitle: string
  /** 0-100, -1 when unknown. */
  percent: number
  lines: string[]
  error?: string
  startedAt: string
  finishedAt?: string
}

export type PowerAction = "start" | "stop" | "restart" | "kill"

// --- Console WebSocket frames -------------------------------------------------

export type ConsoleServerFrame =
  | { type: "hello"; state: ServerState; pid?: number; startedAt?: string }
  | { type: "output"; data: string }
  | { type: "state"; state: ServerState; exitCode?: number; message?: string }
  | { type: "error"; message: string }

export type ConsoleClientFrame = { type: "input"; data: string } | { type: "resize"; cols: number; rows: number }
