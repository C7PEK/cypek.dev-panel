import { BotIcon, BoxIcon, CarIcon, TerminalIcon } from "lucide-react"

import type { ServerCategory } from "@/lib/types"

export function CategoryIcon({ category, className }: { category: ServerCategory; className?: string }) {
  switch (category) {
    case "fivem":
      return <CarIcon className={className} />
    case "minecraft":
      return <BoxIcon className={className} />
    case "discord-bot":
      return <BotIcon className={className} />
    default:
      return <TerminalIcon className={className} />
  }
}
