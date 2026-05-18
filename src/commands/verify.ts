import { join } from "node:path"
import { mkdir, mkdtemp, readFile, rm, stat } from "node:fs/promises"
import { tmpdir } from "node:os"
import { resolveStorage } from "./storage"
import { sha256File } from "../core/checksum"
import { assertValidBackupId } from "../core/ids"
import { decryptManifestFromFile } from "../core/manifest-crypto"
import { validateLocatorV2 } from "../core/locator"
import { concatenateChunks } from "../core/chunk"
import type { ManifestV2 } from "../core/manifest"
import { createProgressBar } from "../core/progress"

export async function loadManifest(backupId: string, from: string, tempDir: string, identity?: string): Promise<ManifestV2> {
  assertValidBackupId(backupId)
  if (!identity) throw new Error("Identity is required to decrypt encrypted manifest v2")
  const { backend } = resolveStorage(from)

  const locatorFile = join(tempDir, "locator.json")
  await backend.downloadFile(`${backupId}/locator.json`, locatorFile)
  const locator = validateLocatorV2(JSON.parse(await readFile(locatorFile, "utf8")), backupId)
  if (!locator.ok) throw new Error(`Invalid locator: ${locator.errors.join("; ")}`)

  const manifestFile = join(tempDir, "manifest.age")
  await backend.downloadFile(`${backupId}/manifest.age`, manifestFile)
  return decryptManifestFromFile(manifestFile, identity, backupId)
}

export async function verifyBackup(options: { backupId: string; from: string; identity?: string }): Promise<{ ok: boolean; errors: string[] }> {
  const tempDir = await mkdtemp(join(tmpdir(), "securebackup-verify-"))
  const chunksDir = join(tempDir, "chunks")
  const encryptedFile = join(tempDir, "payload.age")
  const errors: string[] = []

  try {
    try {
      assertValidBackupId(options.backupId)
    } catch (error) {
      return { ok: false, errors: [(error as Error).message] }
    }

    const { backend } = resolveStorage(options.from)
    let manifest: ManifestV2
    try {
      manifest = await loadManifest(options.backupId, options.from, tempDir, options.identity)
    } catch (error) {
      return { ok: false, errors: [`Missing or invalid encrypted manifest: ${(error as Error).message}`] }
    }

    await mkdir(chunksDir, { recursive: true, mode: 0o700 })

    const downloadBar = createProgressBar({ total: manifest.chunks.length, label: "Downloading chunks" })
    for (const chunk of manifest.chunks) {
      const remotePath = `${options.backupId}/chunks/${chunk.name}`
      if (!(await backend.exists(remotePath))) {
        errors.push(`Missing chunk: ${chunk.name}`)
        downloadBar.increment()
        continue
      }
      await backend.downloadFile(remotePath, join(chunksDir, chunk.name))
      downloadBar.increment()
    }
    downloadBar.stop()

    const bar = createProgressBar({ total: manifest.chunks.length, label: "Verifying chunks" })
    for (const chunk of manifest.chunks) {
      const localPath = join(chunksDir, chunk.name)
      try {
        const info = await stat(localPath)
        if (info.size !== chunk.size) errors.push(`Size mismatch for ${chunk.name}: expected ${chunk.size}, got ${info.size}`)
        const hash = await sha256File(localPath)
        if (hash !== chunk.sha256) errors.push(`Checksum mismatch for ${chunk.name}: expected ${chunk.sha256}, got ${hash}`)
      } catch {
        errors.push(`Missing chunk: ${chunk.name}`)
      }
      bar.increment()
    }
    bar.stop()

    if (errors.length === 0) {
      const joinBar = createProgressBar({ total: manifest.chunks.length, label: "Joining chunks" })
      await concatenateChunks(manifest.chunks, chunksDir, encryptedFile, (c) => joinBar.update(c))
      joinBar.stop()
      const encryptedHash = await sha256File(encryptedFile)
      if (encryptedHash !== manifest.payload.encrypted_file_sha256) {
        errors.push(`Encrypted file checksum mismatch: expected ${manifest.payload.encrypted_file_sha256}, got ${encryptedHash}`)
      }
    }

    return { ok: errors.length === 0, errors }
  } finally {
    await rm(tempDir, { recursive: true, force: true })
  }
}
