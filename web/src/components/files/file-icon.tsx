import {
  FileArchiveIcon,
  FileCode2Icon,
  FileIcon,
  FileJsonIcon,
  FileTextIcon,
  FolderIcon,
  ImageIcon,
  LinkIcon,
  PackageIcon,
} from "lucide-react"

import type { FileEntry } from "@/lib/files"
import { isArchive } from "@/lib/files"

export function FileIconFor({ entry, className }: { entry: FileEntry; className?: string }) {
  if (entry.type === "dir") return <FolderIcon className={className} />
  if (entry.type === "symlink") return <LinkIcon className={className} />
  const n = entry.name.toLowerCase()
  if (isArchive(n)) return <FileArchiveIcon className={className} />
  if (/\.(jar)$/.test(n)) return <PackageIcon className={className} />
  if (/\.(json)$/.test(n)) return <FileJsonIcon className={className} />
  if (/\.(ya?ml|toml|properties|cfg|ini|conf|sh|lua|js|ts|mjs|cjs|py)$/.test(n)) return <FileCode2Icon className={className} />
  if (/\.(txt|md|log)$/.test(n)) return <FileTextIcon className={className} />
  if (/\.(png|jpe?g|gif|webp|svg)$/.test(n)) return <ImageIcon className={className} />
  return <FileIcon className={className} />
}
