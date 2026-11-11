import { TerminalIcon } from "lucide-react"

import type { ServerCategory } from "@/lib/types"
import { cn } from "@/lib/utils"

// Brand marks (Discord, FiveM, Node.js, Python) are the Simple Icons paths,
// CC0-1.0 (https://simpleicons.org). The Minecraft blocks are original
// pixel art: Simple Icons carries no Minecraft mark.
const DISCORD =
  "M20.317 4.3698a19.7913 19.7913 0 00-4.8851-1.5152.0741.0741 0 00-.0785.0371c-.211.3753-.4447.8648-.6083 1.2495-1.8447-.2762-3.68-.2762-5.4868 0-.1636-.3933-.4058-.8742-.6177-1.2495a.077.077 0 00-.0785-.037 19.7363 19.7363 0 00-4.8852 1.515.0699.0699 0 00-.0321.0277C.5334 9.0458-.319 13.5799.0992 18.0578a.0824.0824 0 00.0312.0561c2.0528 1.5076 4.0413 2.4228 5.9929 3.0294a.0777.0777 0 00.0842-.0276c.4616-.6304.8731-1.2952 1.226-1.9942a.076.076 0 00-.0416-.1057c-.6528-.2476-1.2743-.5495-1.8722-.8923a.077.077 0 01-.0076-.1277c.1258-.0943.2517-.1923.3718-.2914a.0743.0743 0 01.0776-.0105c3.9278 1.7933 8.18 1.7933 12.0614 0a.0739.0739 0 01.0785.0095c.1202.099.246.1981.3728.2924a.077.077 0 01-.0066.1276 12.2986 12.2986 0 01-1.873.8914.0766.0766 0 00-.0407.1067c.3604.698.7719 1.3628 1.225 1.9932a.076.076 0 00.0842.0286c1.961-.6067 3.9495-1.5219 6.0023-3.0294a.077.077 0 00.0313-.0552c.5004-5.177-.8382-9.6739-3.5485-13.6604a.061.061 0 00-.0312-.0286zM8.02 15.3312c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9555-2.4189 2.157-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.9555 2.4189-2.1569 2.4189zm7.9748 0c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9554-2.4189 2.1569-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.946 2.4189-2.1568 2.4189Z"
const FIVEM =
  "M22.4 24h-5.225c-.117 0-.455-1.127-1.026-3.375-1.982-6.909-3.124-10.946-3.417-12.12l3.37-3.325h.099c.454 1.42 2.554 7.676 6.299 18.768ZM12.342 7.084h-.048a3.382 3.385 0 0 1-.098-.492v-.098a102.619 102.715 0 0 1 3.272-3.275c.13.196.196.356.196.491v.05a140.694 140.826 0 0 1-3.322 3.324ZM5.994 10.9h-.05c.67-2.12 1.076-3.209 1.223-3.275L14.492.343c.08 0 .258.524.533 1.562zm1.37-4.014h-.05C8.813 2.342 9.612.048 9.71 0h4.495v.05a664.971 664.971 0 0 1-6.841 6.839Zm-2.69 7.874h-.05c.166-.798.554-1.418 1.174-1.855a312.918 313.213 0 0 1 5.71-5.717h.05c-.117.672-.375 1.175-.781 1.52zM1.598 24l-.098-.05c1.399-4.172 2.148-6.322 2.248-6.45l6.74-6.694v.05C10.232 11.88 8.974 16.263 6.73 24Z"
const NODE =
  "M11.998,24c-0.321,0-0.641-0.084-0.922-0.247l-2.936-1.737c-0.438-0.245-0.224-0.332-0.08-0.383 c0.585-0.203,0.703-0.25,1.328-0.604c0.065-0.037,0.151-0.023,0.218,0.017l2.256,1.339c0.082,0.045,0.197,0.045,0.272,0l8.795-5.076 c0.082-0.047,0.134-0.141,0.134-0.238V6.921c0-0.099-0.053-0.192-0.137-0.242l-8.791-5.072c-0.081-0.047-0.189-0.047-0.271,0 L3.075,6.68C2.99,6.729,2.936,6.825,2.936,6.921v10.15c0,0.097,0.054,0.189,0.139,0.235l2.409,1.392 c1.307,0.654,2.108-0.116,2.108-0.89V7.787c0-0.142,0.114-0.253,0.256-0.253h1.115c0.139,0,0.255,0.112,0.255,0.253v10.021 c0,1.745-0.95,2.745-2.604,2.745c-0.508,0-0.909,0-2.026-0.551L2.28,18.675c-0.57-0.329-0.922-0.945-0.922-1.604V6.921 c0-0.659,0.353-1.275,0.922-1.603l8.795-5.082c0.557-0.315,1.296-0.315,1.848,0l8.794,5.082c0.57,0.329,0.924,0.944,0.924,1.603 v10.15c0,0.659-0.354,1.273-0.924,1.604l-8.794,5.078C12.643,23.916,12.324,24,11.998,24z M19.099,13.993 c0-1.9-1.284-2.406-3.987-2.763c-2.731-0.361-3.009-0.548-3.009-1.187c0-0.528,0.235-1.233,2.258-1.233 c1.807,0,2.473,0.389,2.747,1.607c0.024,0.115,0.129,0.199,0.247,0.199h1.141c0.071,0,0.138-0.031,0.186-0.081 c0.048-0.054,0.074-0.123,0.067-0.196c-0.177-2.098-1.571-3.076-4.388-3.076c-2.508,0-4.004,1.058-4.004,2.833 c0,1.925,1.488,2.457,3.895,2.695c2.88,0.282,3.103,0.703,3.103,1.269c0,0.983-0.789,1.402-2.642,1.402 c-2.327,0-2.839-0.584-3.011-1.742c-0.02-0.124-0.126-0.215-0.253-0.215h-1.137c-0.141,0-0.254,0.112-0.254,0.253 c0,1.482,0.806,3.248,4.655,3.248C17.501,17.007,19.099,15.91,19.099,13.993z"
const PYTHON =
  "M14.25.18l.9.2.73.26.59.3.45.32.34.34.25.34.16.33.1.3.04.26.02.2-.01.13V8.5l-.05.63-.13.55-.21.46-.26.38-.3.31-.33.25-.35.19-.35.14-.33.1-.3.07-.26.04-.21.02H8.77l-.69.05-.59.14-.5.22-.41.27-.33.32-.27.35-.2.36-.15.37-.1.35-.07.32-.04.27-.02.21v3.06H3.17l-.21-.03-.28-.07-.32-.12-.35-.18-.36-.26-.36-.36-.35-.46-.32-.59-.28-.73-.21-.88-.14-1.05-.05-1.23.06-1.22.16-1.04.24-.87.32-.71.36-.57.4-.44.42-.33.42-.24.4-.16.36-.1.32-.05.24-.01h.16l.06.01h8.16v-.83H6.18l-.01-2.75-.02-.37.05-.34.11-.31.17-.28.25-.26.31-.23.38-.2.44-.18.51-.15.58-.12.64-.1.71-.06.77-.04.84-.02 1.27.05zm-6.3 1.98l-.23.33-.08.41.08.41.23.34.33.22.41.09.41-.09.33-.22.23-.34.08-.41-.08-.41-.23-.33-.33-.22-.41-.09-.41.09zm13.09 3.95l.28.06.32.12.35.18.36.27.36.35.35.47.32.59.28.73.21.88.14 1.04.05 1.23-.06 1.23-.16 1.04-.24.86-.32.71-.36.57-.4.45-.42.33-.42.24-.4.16-.36.09-.32.05-.24.02-.16-.01h-8.22v.82h5.84l.01 2.76.02.36-.05.34-.11.31-.17.29-.25.25-.31.24-.38.2-.44.17-.51.15-.58.13-.64.09-.71.07-.77.04-.84.01-1.27-.04-1.07-.14-.9-.2-.73-.25-.59-.3-.45-.33-.34-.34-.25-.34-.16-.33-.1-.3-.04-.25-.02-.2.01-.13v-5.34l.05-.64.13-.54.21-.46.26-.38.3-.32.33-.24.35-.2.35-.14.33-.1.3-.06.26-.04.21-.02.13-.01h5.84l.69-.05.59-.14.5-.21.41-.28.33-.32.27-.35.2-.36.15-.36.1-.35.07-.32.04-.28.02-.21V6.07h2.09l.14.01zm-6.47 14.25l-.23.33-.08.41.08.41.23.33.33.23.41.08.41-.08.33-.23.23-.33.08-.41-.08-.41-.23-.33-.33-.23-.41-.08-.41.08z"

function Brand({ d, color, className, title }: { d: string; color: string; className?: string; title: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} role="img" aria-label={title}>
      <path d={d} fill={color} />
    </svg>
  )
}

type Block = { top: string; topEdge: string; left: string; right: string; speck?: string; tiles?: string; fringeL?: string; fringeR?: string }

const GRASS: Block = { top: "#62B246", topEdge: "#7CCB5B", left: "#8D5B32", right: "#6F4425", speck: "#5A3518", fringeL: "#4E9A31", fringeR: "#3E8125" }
const PURPUR: Block = { top: "#C99BD0", topEdge: "#DDB7E2", left: "#A77BAE", right: "#8A6191", tiles: "#7A5482" }
const BEDROCK: Block = { top: "#8E8E8E", topEdge: "#ABABAB", left: "#6B6B6B", right: "#555555", speck: "#353535" }
const CRAFTING: Block = { top: "#B98E58", topEdge: "#D2A86E", left: "#8C6136", right: "#714C28", tiles: "#4E321A" }
const PROXY: Block = { top: "#62C3EE", topEdge: "#8AD5F5", left: "#2F89B0", right: "#226C8D", tiles: "#17526C" }

// An isometric pixel block: grass for Minecraft, purpur for Purpur, a
// crafting table for mod loaders, bedrock for Bedrock, blue for the proxy.
function BlockIcon({ b, className, title }: { b: Block; className?: string; title: string }) {
  return (
    <svg viewBox="0 0 16 16" className={className} role="img" aria-label={title}>
      <polygon points="8,0.8 15.2,4.4 8,8 0.8,4.4" fill={b.top} />
      <polygon points="8,0.8 11.6,2.6 8,4.4 4.4,2.6" fill={b.topEdge} opacity="0.55" />
      <polygon points="0.8,4.4 8,8 8,15.2 0.8,11.6" fill={b.left} />
      <polygon points="8,8 15.2,4.4 15.2,11.6 8,15.2" fill={b.right} />
      {b.fringeL && <polygon points="0.8,4.4 8,8 8,9.8 6.2,8.9 6.2,9.8 4.4,8.9 4.4,7.6 2.6,6.7 2.6,7.6 0.8,6.7" fill={b.fringeL} />}
      {b.fringeR && <polygon points="8,8 15.2,4.4 15.2,6.7 13.4,7.6 13.4,6.7 11.6,7.6 11.6,8.9 9.8,9.8 9.8,8.9 8,9.8" fill={b.fringeR} />}
      {b.speck && (
        <>
          <polygon points="2.6,9.6 3.5,10.05 3.5,10.95 2.6,10.5" fill={b.speck} />
          <polygon points="5.3,11.4 6.2,11.85 6.2,12.75 5.3,12.3" fill={b.speck} />
          <polygon points="11.6,10.95 12.5,10.5 12.5,11.4 11.6,11.85" fill={b.speck} />
          <polygon points="9.8,12.75 10.7,12.3 10.7,13.2 9.8,13.65" fill={b.speck} />
        </>
      )}
      {b.tiles && (
        <g fill="none" stroke={b.tiles} strokeWidth="0.5" opacity="0.8">
          <polygon points="8,2.6 11.6,4.4 8,6.2 4.4,4.4" />
          <polyline points="0.8,8 8,11.6 15.2,8" />
          <line x1="4.4" y1="6.2" x2="4.4" y2="13.4" />
          <line x1="11.6" y1="6.2" x2="11.6" y2="13.4" />
        </g>
      )}
    </svg>
  )
}

/**
 * The game's mark for a server or template: picked from the template id
 * (Purpur, Node/Python bots), falling back to the category (imported
 * templates and custom servers).
 */
export function CategoryIcon({ category, templateId, className }: { category: ServerCategory; templateId?: string; className?: string }) {
  const id = templateId ?? ""
  if (id.includes("purpur")) return <BlockIcon b={PURPUR} className={className} title="Purpur" />
  if (id.includes("bedrock")) return <BlockIcon b={BEDROCK} className={className} title="Minecraft Bedrock" />
  if (id.includes("velocity")) return <BlockIcon b={PROXY} className={className} title="Velocity" />
  if (/forge|fabric|quilt/.test(id)) return <BlockIcon b={CRAFTING} className={className} title="Minecraft (mods)" />
  switch (category) {
    case "minecraft":
      return <BlockIcon b={GRASS} className={className} title="Minecraft" />
    case "fivem":
      return <Brand d={FIVEM} color="#F40552" className={className} title="FiveM" />
    case "discord-bot": {
      const lang = id.includes("python") ? { d: PYTHON, color: "#3776AB", title: "Python" } : id.includes("node") ? { d: NODE, color: "#5FA04E", title: "Node.js" } : null
      return (
        <span className={cn("relative inline-flex shrink-0", className)}>
          <Brand d={DISCORD} color="#5865F2" className="size-full" title="Discord" />
          {lang && (
            <span className="absolute -right-1 -bottom-1 flex size-[55%] items-center justify-center rounded-full bg-card p-[1px] ring-1 ring-border">
              <Brand d={lang.d} color={lang.color} className="size-full" title={lang.title} />
            </span>
          )}
        </span>
      )
    }
    default:
      return <TerminalIcon className={className} />
  }
}
