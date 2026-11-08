import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands"
import { javascript } from "@codemirror/lang-javascript"
import { json } from "@codemirror/lang-json"
import { yaml } from "@codemirror/lang-yaml"
import {
  StreamLanguage,
  bracketMatching,
  defaultHighlightStyle,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language"
import { lua } from "@codemirror/legacy-modes/mode/lua"
import { properties } from "@codemirror/legacy-modes/mode/properties"
import { shell } from "@codemirror/legacy-modes/mode/shell"
import { toml } from "@codemirror/legacy-modes/mode/toml"
import { highlightSelectionMatches, searchKeymap } from "@codemirror/search"
import { Compartment, EditorState, type Extension } from "@codemirror/state"
import {
  EditorView,
  drawSelection,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from "@codemirror/view"
import { useEffect, useRef } from "react"

/** Picks a language extension from the file name; plain text when unknown. */
export function languageFor(name: string): Extension {
  const n = name.toLowerCase()
  if (/\.(ya?ml)$/.test(n)) return yaml()
  if (/\.json$/.test(n)) return json()
  if (/\.(m?js|cjs|ts|mts)$/.test(n)) return javascript({ typescript: /\.m?ts$/.test(n) })
  if (/\.(properties|cfg|ini|conf|env)$/.test(n)) return StreamLanguage.define(properties)
  if (/\.toml$/.test(n)) return StreamLanguage.define(toml)
  if (/\.(sh|bash)$/.test(n)) return StreamLanguage.define(shell)
  if (/\.lua$/.test(n)) return StreamLanguage.define(lua)
  return []
}

// Dark palette mirrors the console: near-black ground, light text, blurple accent.
const darkTheme = EditorView.theme(
  {
    "&": { backgroundColor: "#050506", color: "#f4f4f6", fontSize: "13px", height: "100%" },
    ".cm-content": { caretColor: "#5865f2", fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#5865f2" },
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection": {
      backgroundColor: "rgba(88, 101, 242, 0.35)",
    },
    ".cm-activeLine": { backgroundColor: "rgba(255, 255, 255, 0.03)" },
    ".cm-gutters": { backgroundColor: "#050506", color: "#5f5f69", borderRight: "1px solid rgba(255,255,255,0.08)" },
    ".cm-activeLineGutter": { backgroundColor: "rgba(255, 255, 255, 0.04)", color: "#c9c9d1" },
    ".cm-matchingBracket": { backgroundColor: "rgba(0, 240, 255, 0.18)", outline: "1px solid rgba(0,240,255,0.4)" },
    ".cm-selectionMatch": { backgroundColor: "rgba(88, 101, 242, 0.2)" },
    ".cm-panels": { backgroundColor: "#0e0e11", color: "#f4f4f6", borderTop: "1px solid rgba(255,255,255,0.1)" },
    ".cm-panels input, .cm-panels button": { color: "#f4f4f6", backgroundColor: "#1a1a1f", border: "1px solid rgba(255,255,255,0.14)" },
    ".cm-searchMatch": { backgroundColor: "rgba(0, 240, 255, 0.25)" },
    ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "rgba(255, 0, 200, 0.35)" },
    // The default highlight palette is tuned for light backgrounds; lift it.
    ".cm-line span[class^='ͼ'], .cm-line span[class*=' ͼ']": { filter: "brightness(1.9) saturate(0.85)" },
  },
  { dark: true },
)

const lightTheme = EditorView.theme({
  "&": { fontSize: "13px", height: "100%" },
  ".cm-content": { fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace" },
})

function isDark() {
  return document.documentElement.classList.contains("dark")
}

interface Props {
  fileName: string
  value: string
  onChange: (value: string) => void
  onSave: () => void
  readOnly?: boolean
  className?: string
}

/** CodeMirror 6 editor. `value` is applied only when it differs from the document, so typing stays local. */
export function CodeEditor({ fileName, value, onChange, onSave, readOnly = false, className }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onChangeRef = useRef(onChange)
  const onSaveRef = useRef(onSave)
  const langCompartment = useRef(new Compartment())
  const themeCompartment = useRef(new Compartment())
  const readOnlyCompartment = useRef(new Compartment())
  onChangeRef.current = onChange
  onSaveRef.current = onSave

  // defaultHighlightStyle is the only palette available without adding
  // @lezer/highlight as a direct dependency; the dark theme brightens it via CSS below.
  const themeExt = () => (isDark() ? [darkTheme, syntaxHighlighting(defaultHighlightStyle)] : [lightTheme, syntaxHighlighting(defaultHighlightStyle)])

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const state = EditorState.create({
      doc: value,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        history(),
        drawSelection(),
        indentOnInput(),
        bracketMatching(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        EditorView.lineWrapping,
        keymap.of([
          {
            key: "Mod-s",
            run: () => {
              onSaveRef.current()
              return true
            },
          },
          ...defaultKeymap,
          ...historyKeymap,
          ...searchKeymap,
          indentWithTab,
        ]),
        langCompartment.current.of(languageFor(fileName)),
        themeCompartment.current.of(themeExt()),
        readOnlyCompartment.current.of(EditorState.readOnly.of(readOnly)),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) onChangeRef.current(u.state.doc.toString())
        }),
      ],
    })
    const view = new EditorView({ state, parent: host })
    viewRef.current = view
    // Follow theme toggles without recreating the editor.
    const mo = new MutationObserver(() => view.dispatch({ effects: themeCompartment.current.reconfigure(themeExt()) }))
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })
    return () => {
      mo.disconnect()
      view.destroy()
      viewRef.current = null
    }
    // The editor is created once per file; value changes are synced below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileName])

  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const current = view.state.doc.toString()
    if (current !== value) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: value } })
    }
  }, [value])

  useEffect(() => {
    viewRef.current?.dispatch({ effects: readOnlyCompartment.current.reconfigure(EditorState.readOnly.of(readOnly)) })
  }, [readOnly])

  return <div ref={hostRef} className={className} />
}
