import { BrandMark } from "@/components/brand/mark"
import { splitName, useBranding } from "@/lib/branding"
import { cn } from "@/lib/utils"

export function Logo({ className, size = "md", name }: { className?: string; size?: "md" | "lg"; name?: string }) {
  const b = useBranding()
  const [bold, rest] = splitName(name ?? b.data?.name ?? "")
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <BrandMark className={size === "lg" ? "size-10" : "size-7"} />
      <span
        className={cn(
          "font-heading leading-none tracking-[0.08em] uppercase",
          size === "lg" ? "text-2xl" : "text-lg",
        )}
      >
        <span className="font-semibold text-foreground">{bold}</span>
        {rest && <span className="font-light text-muted-foreground">{rest}</span>}
      </span>
    </span>
  )
}
