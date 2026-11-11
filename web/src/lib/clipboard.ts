export async function copyText(text: string, anchor?: Element | null): Promise<boolean> {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
    }
  }
  const host = (anchor?.closest('[role="dialog"]') as HTMLElement | null) ?? (anchor?.parentElement as HTMLElement | null) ?? document.body
  const previous = document.activeElement as HTMLElement | null
  const ta = document.createElement("textarea")
  ta.value = text
  ta.setAttribute("readonly", "")
  ta.setAttribute("aria-hidden", "true")
  ta.tabIndex = -1
  Object.assign(ta.style, { position: "fixed", top: "0", left: "0", width: "1px", height: "1px", padding: "0", border: "0", opacity: "0", pointerEvents: "none" })
  host.appendChild(ta)
  let ok = false
  try {
    ta.focus({ preventScroll: true })
    ta.select()
    ta.setSelectionRange(0, text.length)
    ok = document.execCommand("copy")
  } catch {
    ok = false
  } finally {
    ta.remove()
    previous?.focus?.({ preventScroll: true })
  }
  return ok
}

export function selectText(el: Element | null) {
  if (!el) return
  const range = document.createRange()
  range.selectNodeContents(el)
  const sel = window.getSelection()
  sel?.removeAllRanges()
  sel?.addRange(range)
}
