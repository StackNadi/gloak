export const MIN_CHUNK_SIZE = 64 * 1024
export const MAX_CHUNK_SIZE = 1024 * 1024 * 1024

export function chunkName(index: number): string {
  return `${String(index).padStart(6, "0")}.chunk`
}

export function validateChunkSize(bytes: number): number {
  if (!Number.isSafeInteger(bytes) || bytes <= 0) throw new Error(`Invalid size: ${bytes}`)
  if (bytes < MIN_CHUNK_SIZE) throw new Error(`Chunk size must be at least 64KiB`)
  if (bytes > MAX_CHUNK_SIZE) throw new Error(`Chunk size must be at most 1GiB`)
  return bytes
}

export function parseSize(value: string | number): number {
  if (typeof value === "number") return validateChunkSize(value)
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
  try {
    return validateChunkSize(bytes)
  } catch (error) {
    throw new Error(`${(error as Error).message}: ${value}`)
  }
}
