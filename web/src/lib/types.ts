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
