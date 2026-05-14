export function chunkName(index: number): string {
  return `${String(index).padStart(6, "0")}.chunk`
}

export function parseSize(value: string | number): number {
  if (typeof value === "number") return value
  const match = value.trim().toLowerCase().match(/^(\d+(?:\.\d+)?)\s*(b|kb|mb|gb|kib|mib|gib)?$/)
  if (!match) throw new Error(`Invalid size: ${value}`)
  const amount = Number(match[1])
  const unit = match[2] ?? "b"
  const units: Record<string, number> = {
    b: 1,
    kb: 1000,
    mb: 1024 * 1024,
    gb: 1024 * 1024 * 1024,
    kib: 1024,
    mib: 1024 * 1024,
    gib: 1024 * 1024 * 1024,
  }
  const bytes = Math.floor(amount * units[unit])
  if (!Number.isSafeInteger(bytes) || bytes <= 0) throw new Error(`Invalid size: ${value}`)
  return bytes
}
