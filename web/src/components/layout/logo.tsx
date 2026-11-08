import { cn } from "@/lib/utils"

export function Logo({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2 font-semibold tracking-tight", className)}>
      <span className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground text-sm font-bold">
        H
      </span>
      HomePanel
    </span>
  )
}
