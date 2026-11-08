import { useSyncExternalStore } from "react"

import { api } from "@/lib/api"
import type { EventsMessage, HostStats, LiveServer } from "@/lib/types"

// One events socket per page, shared by every component that calls
// useLiveStats(). Opens on the first subscriber, closes after the last one
// leaves; while the socket is down the host card is fed by polling.

export interface LiveStats {
  host?: HostStats
  servers: Record<string, LiveServer>
  connected: boolean
}

const HISTORY_MAX = 60
const BACKOFF_MAX_MS = 15_000
const POLL_MS = 5_000

let state: LiveStats = { servers: {}, connected: false }
const listeners = new Set<() => void>()
let ws: WebSocket | null = null
let refs = 0
let attempt = 0
let reconnectTimer: ReturnType<typeof setTimeout> | undefined
let pollTimer: ReturnType<typeof setInterval> | undefined
let closing = false

function emit(next: LiveStats) {
  state = next
  for (const l of listeners) l()
}

function eventsUrl() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${location.host}/api/v1/events`
}

function applyMessage(m: EventsMessage) {
  const servers: Record<string, LiveServer> = { ...state.servers }
  const seen = new Set<string>()
  for (const s of m.servers) {
    seen.add(s.id)
    const prev = servers[s.id]
    let history: LiveServer["history"]
    if (m.type === "snapshot") {
      history = s.history ?? []
      if (history.length === 0 || history[history.length - 1].at !== s.sample.at) history = [...history, s.sample]
    } else {
      history = [...(prev?.history ?? []), s.sample]
    }
    if (history.length > HISTORY_MAX) history = history.slice(-HISTORY_MAX)
    servers[s.id] = { status: { ...prev?.status, ...s.status }, sample: s.sample, players: s.players, history }
  }
  if (m.type === "snapshot") {
    for (const id of Object.keys(servers)) if (!seen.has(id)) delete servers[id]
  }
  emit({ host: m.host, servers, connected: true })
}

function startPolling() {
  if (pollTimer) return
  const poll = () => {
    api<HostStats>("GET", "/system/stats")
      .then((host) => emit({ ...state, host }))
      .catch(() => {})
  }
  poll()
  pollTimer = setInterval(poll, POLL_MS)
}

function stopPolling() {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = undefined
}

function connect() {
  if (ws || refs === 0) return
  closing = false
  const sock = new WebSocket(eventsUrl())
  ws = sock
  sock.onopen = () => {
    attempt = 0
    stopPolling()
  }
  sock.onmessage = (ev) => {
    if (typeof ev.data !== "string") return
    try {
      applyMessage(JSON.parse(ev.data) as EventsMessage)
    } catch {
      // ignore malformed frames
    }
  }
  sock.onclose = () => {
    if (ws === sock) ws = null
    emit({ ...state, connected: false })
    if (closing || refs === 0) return
    startPolling()
    const delay = Math.min(1000 * 2 ** attempt, BACKOFF_MAX_MS)
    attempt++
    reconnectTimer = setTimeout(connect, delay)
  }
}

function disconnect() {
  closing = true
  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = undefined
  stopPolling()
  if (ws) {
    const sock = ws
    ws = null
    sock.onclose = null
    sock.close(1000)
  }
  emit({ ...state, connected: false })
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  refs++
  if (refs === 1) connect()
  return () => {
    listeners.delete(listener)
    refs--
    if (refs === 0) disconnect()
  }
}

function getSnapshot() {
  return state
}

/** Live host and server stats from the events socket (polling fallback for the host). */
export function useLiveStats(): LiveStats {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}
