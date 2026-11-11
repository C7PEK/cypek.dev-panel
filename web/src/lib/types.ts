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

export type ServerState = "stopped" | "starting" | "running" | "stopping" | "crashed" | "unknown"

export type ServerCategory = "fivem" | "minecraft" | "discord-bot" | "other"

export interface ServerStatus {
  state: ServerState
  pid?: number
  since?: string
  exitCode?: number
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

export interface Limits {
  memoryMb: number
  cpuPercent: number
}

export type InstallState = "pending" | "installing" | "ready" | "failed"

export interface Players {
  online: number
  max: number
  names?: string[]
  kind: string
  at: string
}

export interface ConsoleCommand {
  name: string
  args?: string
  help?: LocalizedText
}

export interface ConsoleHints {
  commands: ConsoleCommand[]
  players?: Players
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
  installState: InstallState
  installError?: string
  createdBy?: number
  createdAt: string
  updatedAt: string
  status: ServerStatus
  players?: Players
  gitUpdate?: boolean
  expiresAt?: string
  expired?: boolean
  expiredAt?: string
}

export type ExpiryInput = { extendDays: number } | { expiresAt: string | null }

export interface ExpiryInfo {
  expiresAt?: string
  expired: boolean
  expiredAt?: string
}

export interface CreateServerInput {
  name: string
  run?: RunConfig
  templateId?: string
  variables?: Record<string, string>
  limits?: Limits
  autostart?: boolean
  ports?: Port[]
  forcePorts?: boolean
  expiresInDays?: number
}

export type UpdateServerInput = Partial<Pick<Server, "name" | "run" | "limits" | "autostart" | "ports" | "variables">> & { forcePorts?: boolean }

export interface PortConflict {
  port: number
  protocol: string
  name: string
  serverId?: string
  serverName?: string
  running: boolean
  host: boolean
  suggestion?: number
}

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
  consent?: boolean
  hidden?: boolean
}

export interface TemplatePort {
  name: string
  port: string
  protocol: string
}

export interface Template {
  id: string
  name: string
  category: ServerCategory
  icon?: string
  group?: string
  order?: number
  description?: LocalizedText
  variables: TemplateVariable[]
  runtime: { kind: string; version?: string }
  ports?: TemplatePort[]
  builtin: boolean
  source?: TemplateSource
  editable?: boolean
  overridesBuiltin?: boolean
  usesScript?: boolean
}

export type TemplateSourceKind = "builtin" | "custom" | "pterodactyl" | "pelican" | "pufferpanel"

export interface TemplateSource {
  kind: TemplateSourceKind
  importedAt?: string
  original?: string
  author?: string
  url?: string
  warnings?: string[]
}

export interface TemplateYaml {
  yaml: string
  editable: boolean
  builtin: boolean
  overridesBuiltin: boolean
}

export interface TemplateImportResult {
  yaml: string
  format: "pterodactyl" | "pelican" | "pufferpanel"
  warnings: string[]
  template: Template
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
  percent: number
  lines: string[]
  error?: string
  startedAt: string
  finishedAt?: string
}

export type PowerAction = "start" | "stop" | "restart" | "kill"

export type ConsoleServerFrame =
  | { type: "hello"; state: ServerState; pid?: number; startedAt?: string }
  | { type: "output"; data: string }
  | { type: "state"; state: ServerState; exitCode?: number; message?: string }
  | { type: "error"; message: string }

export type ConsoleClientFrame = { type: "input"; data: string } | { type: "resize"; cols: number; rows: number }

export interface HostStats {
  at: string
  cpuPercent: number
  cpuCount: number
  load1: number
  memTotal: number
  memUsed: number
  memAvailable: number
  diskPath: string
  diskTotal: number
  diskUsed: number
  diskFree: number
  uptimeSeconds: number
  hostname: string
  os: string
}

export interface StatsSample {
  at: string
  state: ServerState
  memoryBytes: number
  cpuPercent: number
  players?: number
}

export interface ServerStatsResponse {
  history: StatsSample[]
  summary: {
    samples: number
    avgCpuPercent: number
    peakCpuPercent: number
    avgMemoryBytes: number
    peakMemoryBytes: number
    peakPlayers: number
  }
  intervalSeconds: number
  memoryLimit: number
  cpuCount: number
}

export type ServerEventKind = "start" | "ready" | "stop" | "exit" | "crash" | "error" | "action"

export interface ServerEvent {
  at: string
  kind: ServerEventKind
  message?: string
  lines?: string[]
  count?: number
  exitCode?: number
  source: "server" | "panel"
  username?: string
}

export interface ServerEventsResponse {
  items: ServerEvent[]
  last24h: { crash: number; error: number; start: number }
}

export interface LiveServer {
  status: ServerStatus
  sample: StatsSample
  players?: Players
  history: StatsSample[]
}

export interface EventsMessage {
  type: "snapshot" | "tick"
  host: HostStats
  servers: { id: string; status: ServerStatus; sample: StatsSample; players?: Players; history?: StatsSample[] }[]
}

export type ScheduleActionType = "warn" | "command" | "wait" | "stop" | "start" | "restart" | "kill" | "backup"

export interface ScheduleAction {
  type: ScheduleActionType
  message?: string
  text?: string
  seconds?: number
}

export interface Schedule {
  id: number
  serverId: string
  name: string
  cron: string
  enabled: boolean
  actions: ScheduleAction[]
  onlyIfRunning: boolean
  skipIfPlayers: boolean
  createdAt: string
  updatedAt: string
  lastRunAt?: string
  nextRunAt?: string
  running: boolean
}

export interface ScheduleInput {
  name?: string
  cron?: string
  enabled?: boolean
  actions?: ScheduleAction[]
  onlyIfRunning?: boolean
  skipIfPlayers?: boolean
}

export type ScheduleRunResult = "running" | "ok" | "skipped" | "failed"

export interface ScheduleRun {
  id: number
  scheduleId: number
  startedAt: string
  finishedAt?: string
  result: ScheduleRunResult
  trigger: "cron" | "manual"
  log: string
}

export interface CronPreview {
  next: string[]
  timezone: string
}

export type Permission =
  | "console.read"
  | "console.write"
  | "power"
  | "files.read"
  | "files.write"
  | "schedules"
  | "backups"
  | "settings"
  | "delete"

export interface TotpStatus {
  enabled: boolean
  backupCodesLeft: number
  required: boolean
  policy: "none" | "admins" | "all"
}

export interface TotpSetup {
  secret: string
  otpauthUrl: string
  qrPng: string
}

export interface ApiKey {
  id: number
  userId: number
  name: string
  prefix: string
  scope: string[]
  createdAt: string
  expiresAt?: string
  lastUsedAt?: string
}

export interface AuditEntry {
  id: number
  createdAt: string
  userId?: number
  username: string
  action: string
  target: string
  ip: string
  details: Record<string, unknown>
}

export type TotpPolicy = "none" | "admins" | "all"

export interface SecuritySettings {
  requireTotp: TotpPolicy
}

export interface UserGrant {
  user: User
  permissions: Permission[]
}

export type LoginResult = SessionResponse | { totpRequired: true; token: string }

export type ModPlatformKind = "plugins" | "mods" | ""

export interface ModPlatform {
  kind: ModPlatformKind
  loader: string
  gameVersion: string
  dir: string
  detected: boolean
  overridden: boolean
  loaders: string[]
}

export interface InstalledMod {
  file: string
  enabled: boolean
  size: number
  modifiedAt: string
  project: { id: string; slug: string; title: string; iconUrl?: string } | null
  version: { id: string; number: string } | null
}

export interface ModsList {
  platform: ModPlatform
  installed: InstalledMod[]
}

export interface ModUpdate {
  versionId: string
  number: string
  name: string
}

export type ModSort = "relevance" | "downloads" | "updated" | "newest"

export interface ModSearchHit {
  projectId: string
  slug: string
  title: string
  description: string
  author: string
  iconUrl?: string
  downloads: number
  categories: string[]
  installed: boolean
  url: string
}

export interface ModSearchPage {
  hits: ModSearchHit[]
  total: number
  offset: number
  limit: number
}

export interface ModVersion {
  id: string
  number: string
  name: string
  type: "release" | "beta" | "alpha"
  date: string
  gameVersions: string[]
}

export interface ModInstallResult {
  installed: { file: string; title: string; version: string; dependency: boolean }[]
  removed: string[]
  restartNeeded: boolean
}

export type AlertKind = "down" | "recovered" | "restarted" | "error" | "test" | "expired" | "expiring"

export interface AlertSettings {
  webhookSet: boolean
  webhookHint: string
  mention: string
  graceSec: number
  notifyRecovered: boolean
  notifyAutoRestart: boolean
  notifyErrors: boolean
  includeLogs: boolean
  language: "pl" | "en" | "de"
  panelUrl: string
}

export interface AlertHistoryItem {
  at: string
  serverId: string
  serverName: string
  kind: AlertKind
  ok: boolean
  error?: string
}

export interface AlertSettingsInput {
  webhook?: string
  mention: string
  graceSec: number
  notifyRecovered: boolean
  notifyAutoRestart: boolean
  notifyErrors: boolean
  includeLogs: boolean
  language: "pl" | "en" | "de"
  panelUrl: string
}

export interface ServerAlerts {
  enabled: boolean
  webhookSet: boolean
  webhookHint: string
  mention: string
  globalWebhookSet: boolean
}

export interface ServerAlertsInput {
  enabled: boolean
  webhook?: string
  mention: string
}

export interface FirewallInfo {
  firewall: { kind: "ufw" | "firewalld" | "none" | "unknown"; active: boolean }
  ports: { port: number; protocol: string; commands: string[] }[]
}

export interface PanelUpdateRun {
  state: "running" | "done" | "uptodate" | "failed"
  from: string
  to?: string
  error?: string
  startedAt: string
  finishedAt?: string
}

export interface PanelUpdateInfo {
  current: string
  repo: string
  latest?: string
  notes?: string
  publishedAt?: string
  available?: boolean
  error?: string
  canUpdate: boolean
  running?: boolean
  last?: PanelUpdateRun
}

export type FileJobKind = "download" | "extract"
export type FileJobState = "running" | "done" | "failed" | "cancelled"
export type FileJobSource = "gdrive" | "dropbox" | "onedrive" | "github" | "direct"

export interface FileJob {
  id: string
  kind: FileJobKind
  state: FileJobState
  phase: "downloading" | "extracting"
  name: string
  source?: FileJobSource
  dir: string
  file?: string
  bytes: number
  total: number
  error?: string
  startedAt: string
  finishedAt?: string
}

export interface FetchInput {
  url: string
  path: string
  extract: boolean
  deleteArchive: boolean
}
