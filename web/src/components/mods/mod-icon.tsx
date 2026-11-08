import { PuzzleIcon } from "lucide-react"
import { useState } from "react"

import { cn } from "@/lib/utils"

/** Project icon from Modrinth with a puzzle fallback (missing or broken image). */
export function ModIcon({ url, className }: { url?: string; className?: string }) {
  const [broken, setBroken] = useState(false)
  return (
    <span className={cn("flex shrink-0 items-center justify-center overflow-hidden rounded-lg border bg-muted/40", className)}>
      {url && !broken ? (
        <img src={url} alt="" loading="lazy" className="size-full object-cover" onError={() => setBroken(true)} referrerPolicy="no-referrer" />
      ) : (
        <PuzzleIcon className="size-1/2 text-muted-foreground" />
      )}
    </span>
  )
}
