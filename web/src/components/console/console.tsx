import { FitAddon } from "@xterm/addon-fit"
import { Terminal } from "@xterm/xterm"
import "@xterm/xterm/css/xterm.css"
import { ArrowDownIcon, ChevronDownIcon, ChevronUpIcon, SearchIcon, SendHorizontalIcon, UsersIcon, XIcon } from "lucide-react"
import { type KeyboardEvent, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import i18n from "@/i18n"
import { analyse, playerMatches, replaceToken, usage } from "@/lib/console-grammar"
import { useConsoleHints } from "@/lib/servers"
import { localized } from "@/lib/templates"
import type { ConsoleClientFrame, ConsoleCommand, ConsoleServerFrame, ServerState } from "@/lib/types"
import { cn } from "@/lib/utils"

type ConnStatus = "connecting" | "connected" | "reconnecting" | "noPermission"

const HISTORY_MAX = 100
const BACKOFF_MAX_MS = 10_000
const CLOSE_NO_PERMISSION = 4403

const historyKey = (id: string) => `homepanel-history-${id}`

function loadHistory(id: string): string[] {
  try {
    const raw = localStorage.getItem(historyKey(id))
    const parsed: unknown = raw ? JSON.parse(raw) : []
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === "string").slice(-HISTORY_MAX) : []
  } catch {
    return []
  }
}

function saveHistory(id: string, items: string[]) {
  try {
    localStorage.setItem(historyKey(id), JSON.stringify(items.slice(-HISTORY_MAX)))
  } catch {
    // Storage may be unavailable (private mode, quota); history then lives in memory only.
  }
}

function consoleUrl(id: string) {
  const proto = location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${location.host}/api/v1/servers/${encodeURIComponent(id)}/console`
}

// Dim "[panel] ..." line printed between game output when the state changes.
function panelLine(text: string) {
  return `\r\n\x1b[2m${text}\x1b[0m\r\n`
}

function stateMessage(state: ServerState, exitCode?: number, message?: string): string {
  let text: string
  switch (state) {
    case "stopped":
      text = exitCode === undefined ? i18n.t("console.msg.stopped") : i18n.t("console.msg.stoppedCode", { code: exitCode })
      break
    case "crashed":
      text = exitCode === undefined ? i18n.t("console.msg.crashedNoCode") : i18n.t("console.msg.crashed", { code: exitCode })
      break
    default:
      text = i18n.t(`console.msg.${state}`)
  }
  return message ? `${text} ${message}` : text
}

const isLive = (s: ServerState) => s === "running" || s === "starting"

interface Match {
  row: number
  col: number
  len: number
}

const MAX_MATCHES = 2000

/** Scrolls the match to the middle of the viewport and selects it. */
function revealMatch(term: Terminal, m: Match) {
  term.scrollToLine(Math.max(0, m.row - Math.floor(term.rows / 2)))
  term.select(m.col, m.row, m.len)
}

/** Case-insensitive search over the whole buffer (scrollback included). */
function findMatches(term: Terminal, query: string): Match[] {
  const needle = query.toLowerCase()
  if (!needle) return []
  const buf = term.buffer.active
  const out: Match[] = []
  for (let row = 0; row < buf.length && out.length < MAX_MATCHES; row++) {
    const line = buf.getLine(row)?.translateToString(true).toLowerCase()
    if (!line) continue
    let i = line.indexOf(needle)
    while (i !== -1 && out.length < MAX_MATCHES) {
      out.push({ row, col: i, len: needle.length })
      i = line.indexOf(needle, i + needle.length)
    }
  }
  return out
}

interface Props {
  serverId: string
  /** State known from the REST API; the socket's own state frames take over once connected. */
  initialState: ServerState
  className?: string
}

export function Console({ serverId, initialState, className }: Props) {
  const { t } = useTranslation()
  const hostRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const [conn, setConn] = useState<ConnStatus>("connecting")
  const [state, setState] = useState<ServerState>(initialState)
  const [atBottom, setAtBottom] = useState(true)
  const [input, setInput] = useState("")
  const [history, setHistory] = useState<string[]>(() => loadHistory(serverId))
  // Index into history counted from the newest entry; -1 means "editing a fresh line".
  const histIdx = useRef(-1)
  const draft = useRef("")
  const inputRef = useRef<HTMLInputElement>(null)

  // Search in the console output (Ctrl+F / Cmd+F while the console is focused).
  const [searchOpen, setSearchOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [matches, setMatches] = useState<Match[]>([])
  const [matchIdx, setMatchIdx] = useState(-1)
  const searchRef = useRef<HTMLInputElement>(null)
  const searchState = useRef({ open: false, query: "", current: null as Match | null })

  // Autocompletion: template commands + online players, polled while the server runs.
  const hints = useConsoleHints(serverId, isLive(state))
  const commands = useMemo(() => hints.data?.commands ?? [], [hints.data])
  const players = useMemo(() => hints.data?.players?.names ?? [], [hints.data])
  const helpOf = useCallback((c: ConsoleCommand) => localized(c.help), [])
  const [suggestOpen, setSuggestOpen] = useState(false)
  const [selected, setSelected] = useState(-1)
  // Repeated Tab on an ambiguous nick cycles through the matches.
  const tabCycle = useRef<{ prefix: string; start: number; i: number } | null>(null)
  const analysis = useMemo(() => analyse(input, commands, players, helpOf), [input, commands, players, helpOf])
  const pickable = analysis.suggestions.filter((sg) => !sg.info)
  const listOpen = suggestOpen && input.trim() !== "" && analysis.suggestions.length > 0

  const send = useCallback((frame: ConsoleClientFrame) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(frame))
  }, [])

  // Terminal lifetime: created once per mount, resized with its container.
  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const term = new Terminal({
      disableStdin: true,
      cursorBlink: false,
      cursorStyle: "underline",
      fontSize: 13,
      lineHeight: 1.2,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
      scrollback: 5000,
      theme: {
        background: "#050506",
        foreground: "#f4f4f6",
        cursor: "#00f0ff",
        cursorAccent: "#050506",
        selectionBackground: "rgba(0, 240, 255, 0.25)",
        black: "#1a1a1f",
        brightBlack: "#5c5c66",
      },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    termRef.current = term

    const safeFit = () => {
      try {
        fit.fit()
      } catch {
        // Container has no size yet (hidden tab); next resize will fit.
      }
    }
    safeFit()

    const updateBottom = () => {
      const b = term.buffer.active
      setAtBottom(b.viewportY >= b.baseY)
    }
    const subs = [
      term.onScroll(updateBottom),
      term.onWriteParsed(updateBottom),
      term.onResize(({ cols, rows }) => send({ type: "resize", cols, rows })),
    ]
    const ro = new ResizeObserver(safeFit)
    ro.observe(host)

    return () => {
      ro.disconnect()
      for (const s of subs) s.dispose()
      term.dispose()
      termRef.current = null
    }
  }, [send])

  // Socket lifetime: reconnects with backoff until unmounted or denied.
  useEffect(() => {
    let ws: WebSocket | null = null
    let attempt = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    let disposed = false
    const url = consoleUrl(serverId)

    const write = (s: string) => termRef.current?.write(s)

    const handle = (frame: ConsoleServerFrame) => {
      switch (frame.type) {
        case "hello": {
          // The backend replays its backlog after hello; drop what we had so nothing doubles.
          termRef.current?.reset()
          setState(frame.state)
          const term = termRef.current
          if (term) send({ type: "resize", cols: term.cols, rows: term.rows })
          break
        }
        case "output":
          write(frame.data)
          break
        case "state":
          setState(frame.state)
          write(panelLine(stateMessage(frame.state, frame.exitCode, frame.message)))
          break
        case "error":
          write(panelLine(i18n.t("console.msg.error", { message: frame.message })))
          if (/permission|forbidden|uprawnie/i.test(frame.message)) setConn("noPermission")
          break
      }
    }

    const connect = () => {
      if (disposed) return
      ws = new WebSocket(url)
      wsRef.current = ws
      ws.onopen = () => {
        attempt = 0
        setConn("connected")
      }
      ws.onmessage = (ev) => {
        if (typeof ev.data !== "string") return
        // One JSON frame per message; a batch may still arrive newline-separated.
        for (const line of ev.data.split("\n")) {
          if (!line.trim()) continue
          try {
            handle(JSON.parse(line) as ConsoleServerFrame)
          } catch {
            // Ignore malformed frames rather than killing the console.
          }
        }
      }
      ws.onclose = (ev) => {
        if (wsRef.current === ws) wsRef.current = null
        if (disposed) return
        const denied = ev.code === CLOSE_NO_PERMISSION || (ev.code === 1008 && /permission|forbidden/i.test(ev.reason))
        if (denied) {
          setConn("noPermission")
          return
        }
        setConn("reconnecting")
        write(panelLine(i18n.t("console.msg.disconnected")))
        const delay = Math.min(1000 * 2 ** attempt, BACKOFF_MAX_MS)
        attempt++
        timer = setTimeout(connect, delay)
      }
    }

    setConn("connecting")
    connect()

    return () => {
      disposed = true
      if (timer) clearTimeout(timer)
      if (ws) {
        ws.onclose = null
        ws.close(1000)
      }
      wsRef.current = null
    }
  }, [serverId, send])

  // Switching servers in place: reload that server's history.
  useEffect(() => {
    setHistory(loadHistory(serverId))
    histIdx.current = -1
    setInput("")
  }, [serverId])

  function showMatch(m: Match | undefined) {
    if (!termRef.current || !m) return
    revealMatch(termRef.current, m)
    searchState.current.current = m
  }

  /** Recomputes matches; keepPlace keeps the selected match when output arrives. */
  const runSearch = useCallback((q: string, keepPlace: boolean) => {
    const term = termRef.current
    if (!term) return
    const found = findMatches(term, q)
    setMatches(found)
    if (found.length === 0) {
      setMatchIdx(-1)
      term.clearSelection()
      searchState.current.current = null
      return
    }
    const cur = searchState.current.current
    let idx = found.length - 1 // newest first: most useful in a log
    if (keepPlace && cur) {
      const same = found.findIndex((m) => m.row === cur.row && m.col === cur.col)
      if (same >= 0) idx = same
    }
    setMatchIdx(idx)
    if (!keepPlace || !cur) {
      revealMatch(term, found[idx])
      searchState.current.current = found[idx]
    }
  }, [])

  function step(delta: number) {
    if (matches.length === 0) return
    const next = (matchIdx + delta + matches.length) % matches.length
    setMatchIdx(next)
    showMatch(matches[next])
  }

  function openSearch() {
    setSearchOpen(true)
    searchState.current.open = true
    requestAnimationFrame(() => {
      searchRef.current?.focus()
      searchRef.current?.select()
    })
  }

  function closeSearch() {
    setSearchOpen(false)
    searchState.current = { open: false, query: "", current: null }
    setQuery("")
    setMatches([])
    setMatchIdx(-1)
    const term = termRef.current
    term?.clearSelection()
    term?.scrollToBottom() // resume following the output
  }

  // New output while searching: refresh the counts (debounced).
  useEffect(() => {
    const term = termRef.current
    if (!term) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const sub = term.onWriteParsed(() => {
      if (!searchState.current.open || !searchState.current.query) return
      clearTimeout(timer)
      timer = setTimeout(() => runSearch(searchState.current.query, true), 300)
    })
    return () => {
      clearTimeout(timer)
      sub.dispose()
    }
  }, [runSearch])

  const canSend = conn === "connected" && isLive(state)

  function setText(next: string) {
    setInput(next)
    histIdx.current = -1
    tabCycle.current = null
  }

  /** Applies a suggestion to the token under the caret. */
  function apply(value: string) {
    setText(replaceToken(input, analysis.tokenStart, value))
    setSelected(-1)
    setSuggestOpen(true)
    inputRef.current?.focus()
  }

  /** Inserts a nick at the caret (from the players row). */
  function insertNick(nick: string) {
    const el = inputRef.current
    const pos = el?.selectionStart ?? input.length
    const before = input.slice(0, pos)
    const after = input.slice(pos)
    const sep = before && !/\s$/.test(before) ? " " : ""
    setText(before + sep + nick + (after.startsWith(" ") || after === "" ? "" : " ") + after)
    el?.focus()
  }

  /** Tab: complete the highlighted suggestion, else the only candidate, else cycle nick matches. */
  function completeTab() {
    if (listOpen && selected >= 0 && pickable[selected]) {
      apply(pickable[selected].value)
      return
    }
    if (listOpen && pickable.length === 1) {
      apply(pickable[0].value)
      return
    }
    const cycle = tabCycle.current
    const word = cycle && cycle.start === analysis.tokenStart ? cycle.prefix : analysis.prefix
    const matches = playerMatches(word, players)
    if (matches.length === 0) {
      if (listOpen && pickable.length > 0) setSelected((i) => (i + 1) % pickable.length)
      return
    }
    const i = cycle && cycle.start === analysis.tokenStart ? (cycle.i + 1) % matches.length : 0
    setInput(replaceToken(input, analysis.tokenStart, matches[i] + (matches.length === 1 ? " " : "")))
    histIdx.current = -1
    tabCycle.current = { prefix: word, start: analysis.tokenStart, i }
  }

  function submit() {
    const text = input
    if (!canSend || !text.trim()) return
    setSuggestOpen(false)
    setSelected(-1)
    tabCycle.current = null
    send({ type: "input", data: text + "\n" })
    setHistory((h) => {
      const next = h[h.length - 1] === text ? h : [...h, text].slice(-HISTORY_MAX)
      saveHistory(serverId, next)
      return next
    })
    histIdx.current = -1
    draft.current = ""
    setInput("")
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Tab" && !e.shiftKey && input.trim() !== "") {
      e.preventDefault()
      completeTab()
      return
    }
    if (e.key === "Escape" && listOpen) {
      e.preventDefault()
      setSuggestOpen(false)
      setSelected(-1)
      return
    }
    if (e.key === "Enter") {
      e.preventDefault()
      if (listOpen && selected >= 0 && pickable[selected]) {
        apply(pickable[selected].value)
        return
      }
      submit()
      return
    }
    if ((e.key === "ArrowUp" || e.key === "ArrowDown") && listOpen && pickable.length > 0) {
      e.preventDefault()
      setSelected((i) => {
        if (e.key === "ArrowDown") return (i + 1) % pickable.length
        return i <= 0 ? pickable.length - 1 : i - 1
      })
      return
    }
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      if (history.length === 0) return
      e.preventDefault()
      if (e.key === "ArrowUp") {
        if (histIdx.current === -1) draft.current = input
        histIdx.current = Math.min(histIdx.current + 1, history.length - 1)
        setInput(history[history.length - 1 - histIdx.current] ?? "")
      } else {
        if (histIdx.current === -1) return
        histIdx.current -= 1
        setInput(histIdx.current === -1 ? draft.current : (history[history.length - 1 - histIdx.current] ?? ""))
      }
    }
  }

  const chip = (() => {
    if (conn === "noPermission") return { key: "noPermission", dot: "bg-red-400" }
    if (conn === "reconnecting") return { key: "reconnecting", dot: "bg-amber-400 brand-pulse" }
    if (conn === "connecting") return { key: "connecting", dot: "bg-amber-400 brand-pulse" }
    if (!isLive(state) && state !== "stopping") return { key: "notRunning", dot: "bg-muted-foreground/60" }
    return { key: "connected", dot: "bg-emerald-400 shadow-[0_0_8px_rgb(52_211_153)]" }
  })()

  return (
    <div
      className={cn("flex min-w-0 flex-col gap-2", className)}
      onKeyDownCapture={(e) => {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f") {
          e.preventDefault()
          openSearch()
        }
      }}
    >
      <div className="relative min-h-64 flex-1 overflow-hidden rounded-lg border border-white/10 bg-[#050506] p-2">
        <div ref={hostRef} className="h-full w-full" />
        {searchOpen ? (
          <div className="absolute top-2 right-3 left-3 z-20 flex items-center gap-1 rounded-md border border-white/15 bg-black/85 p-1 shadow-lg backdrop-blur-sm sm:left-auto sm:w-96">
            <SearchIcon className="ml-1 size-3.5 shrink-0 text-zinc-400" />
            <input
              ref={searchRef}
              value={query}
              onChange={(e) => {
                const q = e.target.value
                setQuery(q)
                searchState.current.query = q
                searchState.current.current = null
                runSearch(q, false)
              }}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault()
                  closeSearch()
                  inputRef.current?.focus()
                } else if (e.key === "Enter") {
                  e.preventDefault()
                  step(e.shiftKey ? 1 : -1)
                }
              }}
              placeholder={t("console.search.placeholder")}
              aria-label={t("console.search.placeholder")}
              className="min-w-0 flex-1 bg-transparent px-1 py-1 font-mono text-xs text-zinc-100 outline-none placeholder:text-zinc-500"
              spellCheck={false}
            />
            <span className="shrink-0 px-1 font-mono text-[0.68rem] text-zinc-400" aria-live="polite">
              {query ? (matches.length > 0 ? `${matchIdx + 1} / ${matches.length}${matches.length >= MAX_MATCHES ? "+" : ""}` : t("console.search.none")) : ""}
            </span>
            <Button type="button" size="icon-sm" variant="ghost" className="text-zinc-300" disabled={matches.length === 0} onClick={() => step(-1)} aria-label={t("console.search.prev")} title={t("console.search.prev")}>
              <ChevronUpIcon />
            </Button>
            <Button type="button" size="icon-sm" variant="ghost" className="text-zinc-300" disabled={matches.length === 0} onClick={() => step(1)} aria-label={t("console.search.next")} title={t("console.search.next")}>
              <ChevronDownIcon />
            </Button>
            <Button type="button" size="icon-sm" variant="ghost" className="text-zinc-300" onClick={closeSearch} aria-label={t("console.search.close")} title={t("console.search.close")}>
              <XIcon />
            </Button>
          </div>
        ) : (
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            className="absolute top-1.5 left-3 z-10 text-zinc-400 hover:text-zinc-100"
            onClick={openSearch}
            aria-label={t("console.search.open")}
            title={t("console.search.open")}
          >
            <SearchIcon />
          </Button>
        )}
        {!atBottom && (
          <Button
            size="sm"
            variant="secondary"
            className="absolute right-4 bottom-3 z-10 shadow-lg"
            onClick={() => termRef.current?.scrollToBottom()}
          >
            <ArrowDownIcon />
            {t("console.jumpToBottom")}
          </Button>
        )}
        <span
          hidden={searchOpen}
          className="label-caps absolute top-2 right-3 z-10 inline-flex items-center gap-1.5 rounded-md border border-white/10 bg-black/60 px-2 py-0.5 text-[0.62rem] text-zinc-300 backdrop-blur-sm"
          role="status"
        >
          <span className={cn("size-1.5 rounded-full", chip.dot)} />
          {t(`console.status.${chip.key}`)}
        </span>
      </div>
      {hints.data?.players && (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground" aria-label={t("console.players.title")}>
          <span className="inline-flex items-center gap-1 font-mono">
            <UsersIcon className="size-3.5" />
            {t("console.players.label", { online: hints.data.players.online, max: hints.data.players.max })}
          </span>
          {players.length > 0 ? (
            players.map((n) => (
              <button
                key={n}
                type="button"
                className="rounded border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-[0.7rem] text-foreground hover:bg-muted disabled:opacity-50"
                onClick={() => insertNick(n)}
                disabled={!canSend}
                title={t("console.players.insert")}
              >
                {n}
              </button>
            ))
          ) : (
            <span>· {t("console.players.none")}</span>
          )}
        </div>
      )}
      <form
        className="relative flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        {listOpen && (
          <div
            className="absolute right-0 bottom-full left-0 z-20 mb-1 max-h-64 overflow-y-auto rounded-lg border bg-popover text-popover-foreground shadow-lg ring-1 ring-foreground/10"
            role="listbox"
          >
            {analysis.command && analysis.tokenIndex > 0 && (
              <div className="label-caps border-b px-3 py-1.5 text-[0.62rem] text-muted-foreground">
                {t("console.suggestions.usage")}: <span className="font-mono normal-case tracking-normal">{usage(analysis.command)}</span>
              </div>
            )}
            {analysis.suggestions.map((sg) => {
              const idx = pickable.indexOf(sg)
              const active = !sg.info && idx === selected
              return (
                <div
                  key={sg.kind + sg.label}
                  role={sg.info ? "note" : "option"}
                  aria-selected={active}
                  className={cn(
                    "flex min-h-9 flex-wrap items-baseline gap-x-3 px-3 py-1.5 text-sm",
                    sg.info ? "text-muted-foreground" : "cursor-pointer hover:bg-muted",
                    active && "bg-primary/15",
                  )}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    if (!sg.info) apply(sg.value)
                  }}
                >
                  <span className="font-mono">{sg.label}</span>
                  {sg.detail && <span className="truncate text-xs text-muted-foreground">{sg.detail}</span>}
                  {sg.kind === "player" && <span className="label-caps ml-auto text-[0.58rem] text-brand-cyan">player</span>}
                </div>
              )
            })}
            {analysis.slot?.player && players.length === 0 && pickable.length === 0 && (
              <div className="px-3 py-1.5 text-xs text-muted-foreground">{t("console.suggestions.noPlayers")}</div>
            )}
            <div className="label-caps hidden border-t px-3 py-1 text-[0.58rem] text-muted-foreground/70 sm:block">
              {t("console.suggestions.hint")}
            </div>
          </div>
        )}
        <Input
          ref={inputRef}
          value={input}
          onChange={(e) => {
            setText(e.target.value)
            setSelected(-1)
            setSuggestOpen(true)
          }}
          onBlur={() => setSuggestOpen(false)}
          onFocus={() => setSuggestOpen(true)}
          onKeyDown={onKeyDown}
          disabled={!canSend}
          placeholder={canSend ? t("console.inputPlaceholder") : t("console.inputDisabled")}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          className="font-mono"
          aria-label={t("console.inputPlaceholder")}
        />
        <Button type="submit" disabled={!canSend || !input.trim()} aria-label={t("console.send")}>
          <SendHorizontalIcon />
          <span className="hidden sm:inline">{t("console.send")}</span>
        </Button>
      </form>
    </div>
  )
}
