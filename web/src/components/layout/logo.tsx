import { BrandMark } from "@/components/brand/mark"
import { cn } from "@/lib/utils"

// Wordmark: "panel" in the display face, ".cypek.dev" muted, after the brand mark.
export function Logo({ className, size = "md" }: { className?: string; size?: "md" | "lg" }) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <BrandMark className={size === "lg" ? "size-10" : "size-7"} />
      <span
        className={cn(
          "font-heading leading-none tracking-[0.08em] uppercase",
          size === "lg" ? "text-2xl" : "text-lg",
        )}
      >
        <span className="font-semibold text-foreground">panel</span>
        <span className="font-light text-muted-foreground">.cypek.dev</span>
      </span>
    </span>
  )
}
