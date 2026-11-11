import { useEffect, useRef } from "react"

export function Particles({ className }: { className?: string }) {
  const ref = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = ref.current
    if (!canvas) return
    const ctx = canvas.getContext("2d")
    if (!ctx) return
    const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches
    const pointer = { x: -9999, y: -9999, active: false }
    type Dot = { x: number; y: number; vx: number; vy: number; r: number; a: number; tint: boolean }
    let w = 0
    let h = 0
    let dots: Dot[] = []
    let raf = 0

    const seed = () => {
      const count = Math.max(40, Math.min(130, Math.round((w * h) / 14000)))
      dots = []
      while (dots.length < count) {
        dots.push({
          x: Math.random() * w,
          y: Math.random() * h,
          vx: (Math.random() - 0.5) * 0.28,
          vy: (Math.random() - 0.5) * 0.28,
          r: Math.random() * 1.6 + 0.5,
          a: Math.random() * 0.5 + 0.2,
          tint: Math.random() < 0.22,
        })
      }
    }
    const resize = () => {
      const dpr = Math.min(devicePixelRatio || 1, 2)
      w = canvas.clientWidth
      h = canvas.clientHeight
      canvas.width = Math.floor(w * dpr)
      canvas.height = Math.floor(h * dpr)
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      seed()
    }
    const move = (d: Dot) => {
      d.x += d.vx
      d.y += d.vy
      if (d.x < -20) d.x = w + 20
      if (d.x > w + 20) d.x = -20
      if (d.y < -20) d.y = h + 20
      if (d.y > h + 20) d.y = -20
      if (!pointer.active) return
      const dx = d.x - pointer.x
      const dy = d.y - pointer.y
      const dist = Math.hypot(dx, dy)
      if (dist < 130 && dist > 0.1) {
        const push = (1 - dist / 130) * 0.6
        d.x += (dx / dist) * push
        d.y += (dy / dist) * push
      }
    }
    const link = (ax: number, ay: number, bx: number, by: number, range: number, alpha: number, color: string) => {
      const dist = Math.hypot(ax - bx, ay - by)
      if (dist > range) return
      ctx.beginPath()
      ctx.moveTo(ax, ay)
      ctx.lineTo(bx, by)
      ctx.strokeStyle = `rgba(${color},${(1 - dist / range) * alpha})`
      ctx.stroke()
    }
    const light = !document.documentElement.classList.contains("dark")
    const base = light ? "0,0,0" : "255,255,255"
    const draw = () => {
      ctx.clearRect(0, 0, w, h)
      ctx.lineWidth = 1
      for (const d of dots) {
        move(d)
        ctx.beginPath()
        ctx.arc(d.x, d.y, d.r, 0, Math.PI * 2)
        ctx.fillStyle = d.tint ? `rgba(120,140,255,${d.a})` : `rgba(${base},${d.a * 0.75})`
        ctx.fill()
      }
      for (let i = 0; i < dots.length; i++) {
        for (let j = i + 1; j < dots.length; j++) {
          link(dots[i].x, dots[i].y, dots[j].x, dots[j].y, 130, 0.16, base)
        }
      }
      if (pointer.active) {
        for (const d of dots) link(d.x, d.y, pointer.x, pointer.y, 170, 0.3, "140,158,255")
      }
    }
    const loop = () => {
      draw()
      raf = requestAnimationFrame(loop)
    }
    const onMove = (e: PointerEvent) => {
      const rect = canvas.getBoundingClientRect()
      pointer.x = e.clientX - rect.left
      pointer.y = e.clientY - rect.top
      pointer.active = true
    }
    const onLeave = () => {
      pointer.active = false
    }
    const onVisibility = () => {
      if (document.hidden) {
        cancelAnimationFrame(raf)
        raf = 0
      } else if (!raf && !reduced) {
        loop()
      }
    }

    resize()
    const ro = new ResizeObserver(resize)
    ro.observe(canvas)
    window.addEventListener("pointermove", onMove, { passive: true })
    window.addEventListener("pointerleave", onLeave, { passive: true })
    document.addEventListener("visibilitychange", onVisibility)
    if (reduced) draw()
    else loop()

    return () => {
      cancelAnimationFrame(raf)
      ro.disconnect()
      window.removeEventListener("pointermove", onMove)
      window.removeEventListener("pointerleave", onLeave)
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [])

  return <canvas ref={ref} className={className} aria-hidden="true" />
}
