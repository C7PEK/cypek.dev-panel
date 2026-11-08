import { cn } from "@/lib/utils"

// The cypek.dev mark: </> with cyan/magenta chromatic offset, from cypek.dev's favicon.
export function BrandMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={cn("size-7 shrink-0", className)} aria-hidden="true">
      <rect width="32" height="32" rx="7" fill="#050506" />
      <rect x=".5" y=".5" width="31" height="31" rx="6.5" fill="none" stroke="#fff" strokeOpacity=".14" />
      <g fill="none" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round">
        <g stroke="var(--brand-cyan)" transform="translate(-.6 -.35)">
          <path d="M11 10.5 4.8 16 11 21.5" />
          <path d="M18.9 7.8 13.1 24.2" />
          <path d="M21 10.5 27.2 16 21 21.5" />
        </g>
        <g stroke="var(--brand-magenta)" transform="translate(.6 .35)">
          <path d="M11 10.5 4.8 16 11 21.5" />
          <path d="M18.9 7.8 13.1 24.2" />
          <path d="M21 10.5 27.2 16 21 21.5" />
        </g>
        <g stroke="#fff">
          <path d="M11 10.5 4.8 16 11 21.5" />
          <path d="M18.9 7.8 13.1 24.2" />
          <path d="M21 10.5 27.2 16 21 21.5" />
        </g>
      </g>
    </svg>
  )
}
