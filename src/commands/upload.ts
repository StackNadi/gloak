import { basename, join } from "node:path"
import { mkdtemp, rm, stat, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { newBackupId } from "../core/ids"
import { encryptFile } from "../core/encrypt"
import { splitFileToChunks } from "../core/chunk"
import { sha256File } from "../core/checksum"
import type { Manifest } from "../core/manifest"
import { resolveStorage } from "./storage"

export type UploadOptions = {
  inputFile: string
  recipient: string
  to: string
  chunkSize?: number
}

const APP_VERSION = "0.1.0"
const DEFAULT_CHUNK_SIZE = 20 * 1024 * 1024

export async function uploadBackup(options: UploadOptions): Promise<{ backupId: string; chunks: number; remote: string }> {
  const chunkSize = options.chunkSize ?? DEFAULT_CHUNK_SIZE
  const inputInfo = await stat(options.inputFile)
  if (!inputInfo.isFile()) throw new Error(`Input must be a file: ${options.inputFile}`)

  const backupId = newBackupId()
  const { backend, backendName, root } = resolveStorage(options.to)
  const tempDir = await mkdtemp(join(tmpdir(), "securebackup-"))
  const encryptedFile = join(tempDir, "payload.age")
  const chunksDir = join(tempDir, "chunks")
  const manifestFile = join(tempDir, "manifest.json")

  try {
    await encryptFile(options.inputFile, encryptedFile, options.recipient)
    const encryptedHash = await sha256File(encryptedFile)
    const { chunks } = await splitFileToChunks(encryptedFile, chunksDir, chunkSize)
    const manifest: Manifest = {
      version: 1,
      backup_id: backupId,
      created_at: new Date().toISOString(),
      app: { name: "securebackup", version: APP_VERSION, runtime: "bun" },
      source: { original_filename: basename(options.inputFile), original_size: inputInfo.size },
      encryption: { format: "age", library: "age-encryption", mode: "recipient", recipient: options.recipient },
      chunking: { chunk_size: chunkSize, total_chunks: chunks.length },
      integrity: { encrypted_file_sha256: encryptedHash },
      storage: { backend: backendName, layout: "filesystem-v1" },
      chunks,
    }
    await writeFile(manifestFile, JSON.stringify(manifest, null, 2), { mode: 0o600 })

    for (const chunk of chunks) {
      await backend.uploadFile(join(chunksDir, chunk.name), `${backupId}/chunks/${chunk.name}`)
    }
    // Upload manifest last so partial uploads don't look complete.
    await backend.uploadFile(manifestFile, `${backupId}/manifest.json`)

    return { backupId, chunks: chunks.length, remote: `${options.to}/${backupId}` }
  } finally {
    await rm(tempDir, { recursive: true, force: true })
  }
}
