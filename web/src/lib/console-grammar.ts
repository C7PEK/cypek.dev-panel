import type { ConsoleCommand } from "@/lib/types"

// Parser for the tiny args grammar used by template console hints:
//   <required> [optional] <a|b|c> <player> [reason...] <target-player|x y z>
// Brackets group, so "<target-player|x y z>" is one slot even though it has spaces.

export interface Slot {
  /** Text inside the brackets, e.g. "survival|creative" or "message..." */
  raw: string
  required: boolean
  /** Alternatives split on "|" (single entry when there is no choice). */
  alternatives: string[]
  /** Fixed words the user can pick (alternatives without spaces/dots). */
  choices: string[]
  /** One of the alternatives names a player, e.g. <player>, [player], <target-player>, <player|ip>. */
  player: boolean
  /** <text...> swallows the rest of the line. */
  rest: boolean
}

export function parseArgs(args: string | undefined): Slot[] {
  if (!args) return []
  const slots: Slot[] = []
  let depth = 0
  let buf = ""
  const flush = () => {
    const tok = buf.trim()
    buf = ""
    if (!tok) return
    const required = tok.startsWith("<")
    const inner = tok.replace(/^[<[]/, "").replace(/[>\]]$/, "")
    const rest = inner.endsWith("...")
    const alternatives = inner.replace(/\.\.\.$/, "").split("|").map((a) => a.trim()).filter(Boolean)
    const player = alternatives.some((a) => /player/i.test(a))
    // A choice is a literal word (no spaces, not a placeholder like "number" or "x y z").
    const choices = alternatives.length > 1 ? alternatives.filter((a) => !/\s/.test(a) && !/player|number|ip$/i.test(a)) : []
    slots.push({ raw: inner, required, alternatives, choices, player, rest })
  }
  for (const ch of args) {
    if (ch === "<" || ch === "[") depth++
    if (ch === ">" || ch === "]") depth = Math.max(0, depth - 1)
    if (ch === " " && depth === 0) {
      flush()
      continue
    }
    buf += ch
  }
  flush()
  return slots
}

/** Which slot the token at index (0 = first argument) falls into; null when past the end. */
export function slotAt(slots: Slot[], index: number): Slot | null {
  for (let i = 0; i < slots.length; i++) {
    if (slots[i].rest) return i <= index ? slots[i] : null
    if (i === index) return slots[i]
  }
  return null
}

export interface Suggestion {
  /** Text inserted when picked. */
  value: string
  /** Primary label. */
  label: string
  /** Secondary muted text (args or help). */
  detail?: string
  /** Rows that only explain the slot and cannot be picked. */
  info?: boolean
  kind: "command" | "choice" | "player" | "info"
}

export interface Analysis {
  /** Index of the token under the caret (0 = command name). */
  tokenIndex: number
  /** Prefix typed in that token. */
  prefix: string
  /** Start offset of that token in the input. */
  tokenStart: number
  command?: ConsoleCommand
  slots: Slot[]
  slot: Slot | null
  suggestions: Suggestion[]
}

/** Case-insensitive prefix match that keeps the original casing. */
const startsWith = (s: string, prefix: string) => s.toLowerCase().startsWith(prefix.toLowerCase())

export function analyse(input: string, commands: ConsoleCommand[], players: string[], help: (c: ConsoleCommand) => string): Analysis {
  // Token under the caret is the last one; a trailing space opens a new, empty token.
  const parts = input.split(/\s+/)
  const endsWithSpace = /\s$/.test(input)
  const tokens = parts.filter((p, i) => p !== "" || i === 0)
  const tokenIndex = endsWithSpace ? tokens.filter(Boolean).length : Math.max(0, tokens.filter(Boolean).length - 1)
  const prefix = endsWithSpace ? "" : (tokens.filter(Boolean)[tokenIndex] ?? "")
  const tokenStart = endsWithSpace ? input.length : input.length - prefix.length

  const name = (tokens.filter(Boolean)[0] ?? "").replace(/^\//, "").toLowerCase()
  const command = tokenIndex > 0 ? commands.find((c) => c.name.toLowerCase() === name) : undefined
  const slots = parseArgs(command?.args)
  const slot = command ? slotAt(slots, tokenIndex - 1) : null

  let suggestions: Suggestion[] = []
  if (tokenIndex === 0) {
    const p = prefix.replace(/^\//, "")
    suggestions = commands
      .filter((c) => startsWith(c.name, p))
      .slice(0, 8)
      .map((c) => ({ value: c.name + (c.args ? " " : ""), label: c.name, detail: [c.args, help(c)].filter(Boolean).join("  ·  "), kind: "command" as const }))
  } else if (slot && !slot.rest) {
    if (slot.choices.length > 0) {
      suggestions = slot.choices.filter((c) => startsWith(c, prefix)).map((c) => ({ value: c + " ", label: c, kind: "choice" as const }))
    }
    if (slot.player) {
      suggestions = suggestions.concat(
        players.filter((n) => startsWith(n, prefix)).map((n) => ({ value: n + " ", label: n, kind: "player" as const })),
      )
    }
    if (suggestions.length === 0 && prefix === "") {
      suggestions = [{ value: "", label: (slot.required ? "<" : "[") + slot.raw + (slot.required ? ">" : "]"), info: true, kind: "info" }]
    }
  }
  return { tokenIndex, prefix, tokenStart, command, slots, slot, suggestions: suggestions.slice(0, 8) }
}

/** Human usage line, e.g. "gamemode <survival|creative|adventure|spectator> [player]". */
export function usage(c: ConsoleCommand): string {
  return c.args ? `${c.name} ${c.args}` : c.name
}

/** Replaces the token that starts at tokenStart with value. */
export function replaceToken(input: string, tokenStart: number, value: string): string {
  return input.slice(0, tokenStart) + value
}

/** Player names that start with the current word, for grammar-independent Tab completion. */
export function playerMatches(prefix: string, players: string[]): string[] {
  if (!prefix) return []
  return players.filter((n) => startsWith(n, prefix))
}
