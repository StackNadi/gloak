import { readdir } from "node:fs/promises"
import { existsSync } from "node:fs"
import { join } from "node:path"
import { homedir } from "node:os"

function securebackupDir(): string {
  const candidates = [
    "/home/keraki/.securebackup",
    join(homedir(), ".securebackup"),
  ]
  for (const p of candidates) {
    if (existsSync(p)) return p
  }
  return candidates[0]
}

function checkpointDir(): string {
  return join(securebackupDir(), "checkpoints")
}

function workdirDir(): string {
  return join(securebackupDir(), "workdir")
}

function checkpointPath(backupId: string): string {
  return join(checkpointDir(), `${backupId}.json`)
}

export function workdirPath(backupId: string): string {
  return join(workdirDir(), backupId)
}

export async function ensureCheckpointDir(): Promise<void> {
  const { mkdir } = await import("node:fs/promises")
  await mkdir(checkpointDir(), { recursive: true, mode: 0o700 })
  await mkdir(workdirDir(), { recursive: true, mode: 0o700 })
}

export type CheckpointState = {
  version: 1
  backupId: string
  /** Full destination URI (e.g. rclone:keraaaki:/backups) */
  destination: string
  /** Source file path */
  sourceFile: string
  /** Age recipient */
  recipient: string
  /** Chunk size in bytes */
  chunkSize: number
  /** Total number of encrypted chunks */
  totalChunks: number
  /** Names of chunks successfully uploaded so far */
  uploadedChunkNames: string[]
  /** SHA-256 of the encrypted payload */
  encryptedFileSha256: string
  /** Original filename stripped of enclosing path */
  originalFilename: string
  /** Original file size in bytes */
  originalSize: number
  startedAt: string
  updatedAt: string
}

export async function writeCheckpoint(
  state: CheckpointState,
): Promise<void> {
  await ensureCheckpointDir()
  state.updatedAt = new Date().toISOString()
  const { writeFile } = await import("node:fs/promises")
  await writeFile(checkpointPath(state.backupId), JSON.stringify(state, null, 2), {
    mode: 0o600,
  })
}

export async function readCheckpoint(
  backupId: string,
): Promise<CheckpointState | null> {
  const path = checkpointPath(backupId)
  if (!existsSync(path)) return null
  try {
    const { readFile } = await import("node:fs/promises")
    const raw = await readFile(path, "utf8")
    const parsed = JSON.parse(raw) as CheckpointState
    if (parsed.version !== 1) return null
    return parsed
  } catch {
    return null
  }
}

export async function removeCheckpoint(backupId: string): Promise<void> {
  const { rm } = await import("node:fs/promises")
  await rm(checkpointPath(backupId), { force: true })
}

export async function listCheckpoints(): Promise<CheckpointState[]> {
  if (!existsSync(checkpointDir())) return []
  const { readFile } = await import("node:fs/promises")
  const files = await readdir(checkpointDir())
  const results: CheckpointState[] = []
  for (const file of files) {
    if (!file.endsWith(".json")) continue
    try {
      const raw = await readFile(join(checkpointDir(), file), "utf8")
      const parsed = JSON.parse(raw) as CheckpointState
      if (parsed.version === 1) results.push(parsed)
    } catch {
      // skip corrupt checkpoints
    }
  }
  return results.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
}
