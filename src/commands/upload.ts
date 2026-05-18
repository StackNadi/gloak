import { basename, join } from "node:path"
import { mkdir, readdir, rm, stat, writeFile } from "node:fs/promises"
import { encryptFile } from "../core/encrypt"
import { splitFileToChunks, type ChunkMetadata } from "../core/chunk"
import { sha256File } from "../core/checksum"
import type { ManifestV2 } from "../core/manifest"
import { encryptManifestToFile } from "../core/manifest-crypto"
import { createLocatorV2 } from "../core/locator"
import { newBackupId } from "../core/ids"
import { resolveStorage } from "./storage"
import { createProgressBar } from "../core/progress"
import { consola } from "consola"
import {
  writeCheckpoint,
  readCheckpoint,
  removeCheckpoint,
  workdirPath,
} from "../core/checkpoint"

export type UploadOptions = {
  inputFile: string
  recipient: string
  to: string
  chunkSize?: number
  resumeBackupId?: string
}

const APP_VERSION = "0.1.0"
const DEFAULT_CHUNK_SIZE = 20 * 1024 * 1024

export async function uploadBackup(options: UploadOptions): Promise<{
  backupId: string
  chunks: number
  remote: string
}> {
  const chunkSize = options.chunkSize ?? DEFAULT_CHUNK_SIZE
  const inputInfo = await stat(options.inputFile)
  if (!inputInfo.isFile()) {
    throw new Error(`Input must be a file: ${options.inputFile}`)
  }

  const { backend, backendName } = resolveStorage(options.to)
  const backupId = options.resumeBackupId ?? newBackupId()
  consola.info(`Backup ID: ${backupId}`)
  if (options.resumeBackupId) consola.info(`Resuming upload for backup: ${backupId}`)
  const workDir = workdirPath(backupId)
  const encryptedFile = join(workDir, "payload.age")
  const chunksDir = join(workDir, "chunks")
  const manifestFile = join(workDir, "manifest.age")
  const locatorFile = join(workDir, "locator.json")

  // ── Resume detection ──────────────────────────────────────────
  let persisted: Awaited<ReturnType<typeof readCheckpoint>> = null
  let uploadedSet = new Set<string>()
  let canSkipEncrypt = false
  let existingChunks: ChunkMetadata[] | null = null

  if (options.resumeBackupId) {
    persisted = await readCheckpoint(backupId)
    if (!persisted) {
      throw new Error(
        `No checkpoint found for backup "${backupId}". Run without --resume to create a fresh backup.`,
      )
    }

    // Check if persistent workdir is still intact
    try {
      const hash = await sha256File(encryptedFile)
      if (hash === persisted.encryptedFileSha256) {
        canSkipEncrypt = true
        uploadedSet = new Set(persisted.uploadedChunkNames)

        // Read existing chunk metadata from disk
        const names = (await readdir(chunksDir))
          .filter((n) => /^\d{6}\.chunk$/.test(n))
          .sort()
        existingChunks = await Promise.all(
          names.map(async (name) => {
            const info = await stat(join(chunksDir, name))
            return { index: parseInt(name, 10), name, size: info.size, sha256: "" }
          }),
        )
      }
    } catch {
      // workdir gone or encrypted file invalid — will clean remote and re-do
    }

    if (!canSkipEncrypt) {
      // Clean remote partial if we can't reuse the encrypted payload
      await backend.remove(`${backupId}`).catch(() => {})
    }
  }

  // ── Setup directories ─────────────────────────────────────────
  await mkdir(workDir, { recursive: true, mode: 0o700 })
  await mkdir(chunksDir, { recursive: true, mode: 0o700 })

  // ── Encrypt payload ──────────────────────────────────────────
  if (!canSkipEncrypt) {
    await encryptFile(options.inputFile, encryptedFile, options.recipient)
  }

  const encryptedHash =
    canSkipEncrypt && persisted
      ? persisted.encryptedFileSha256
      : await sha256File(encryptedFile)

  const encryptedSize = (await stat(encryptedFile)).size
  const totalChunks = Math.ceil(encryptedSize / chunkSize)

  // ── Split or re-use existing chunks ──────────────────────────
  let chunks: ChunkMetadata[]
  if (canSkipEncrypt && existingChunks) {
    chunks = existingChunks
  } else {
    const splitBar = createProgressBar({ total: totalChunks, label: "Splitting chunks" })
    const result = await splitFileToChunks(encryptedFile, chunksDir, chunkSize, (current) =>
      splitBar.update(current),
    )
    splitBar.stop()
    chunks = result.chunks
  }

  // ── Check for existing backup (only on fresh uploads) ───────
  if (!options.resumeBackupId) {
    if (
      (await backend.exists(`${backupId}/locator.json`)) ||
      (await backend.exists(`${backupId}/manifest.age`))
    ) {
      throw new Error(`Backup already exists: ${backupId}`)
    }
  }

  // ── Build manifest ───────────────────────────────────────────
  const manifest: ManifestV2 = {
    version: 2,
    backup_id: backupId,
    created_at: persisted?.startedAt ?? new Date().toISOString(),
    app: { name: "securebackup", version: APP_VERSION, runtime: "bun" },
    source: {
      original_filename: persisted?.originalFilename ?? basename(options.inputFile),
      original_size: persisted?.originalSize ?? inputInfo.size,
    },
    payload: {
      encryption: {
        format: "age",
        library: "age-encryption",
        mode: "recipient",
        recipients: [options.recipient],
      },
      encrypted_file_sha256: encryptedHash,
    },
    manifest_encryption: {
      format: "age",
      library: "age-encryption",
      mode: "recipient",
      recipients: [options.recipient],
    },
    chunking: { chunk_size: chunkSize, total_chunks: chunks.length },
    storage: { backend: backendName, layout: "filesystem-v2" },
    chunks,
  }
  await encryptManifestToFile(manifest, manifestFile, options.recipient)
  await writeFile(locatorFile, JSON.stringify(createLocatorV2(backupId), null, 2), { mode: 0o600 })

  // ── Upload chunks (skip already-uploaded on resume) ─────────
  const pendingCount = chunks.length - uploadedSet.size
  const bar = createProgressBar({
    total: pendingCount > 0 ? pendingCount : 1,
    label: "Uploading chunks",
  })
  for (const chunk of chunks) {
    if (uploadedSet.has(chunk.name)) continue
    await backend.uploadFile(join(chunksDir, chunk.name), `${backupId}/chunks/${chunk.name}`)
    uploadedSet.add(chunk.name)
    bar.increment()

    // Persist checkpoint after every successful chunk
    await writeCheckpoint({
      version: 1,
      backupId,
      destination: options.to,
      sourceFile: options.inputFile,
      recipient: options.recipient,
      chunkSize,
      totalChunks: chunks.length,
      uploadedChunkNames: [...uploadedSet],
      encryptedFileSha256: encryptedHash,
      originalFilename: basename(options.inputFile),
      originalSize: inputInfo.size,
      startedAt: persisted?.startedAt ?? new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    }).catch(() => {})
  }
  bar.stop()

  // ── Upload manifest + locator (always last) ──────────────────
  await backend.uploadFile(manifestFile, `${backupId}/manifest.age`)
  await backend.uploadFile(locatorFile, `${backupId}/locator.json`)

  // ── SUCCESS: clean up workdir + checkpoint ───────────────────
  await removeCheckpoint(backupId).catch(() => {})
  await rm(workDir, { recursive: true, force: true }).catch(() => {})

  return { backupId, chunks: chunks.length, remote: `${options.to}/${backupId}` }
}
