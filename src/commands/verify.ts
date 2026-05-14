import { join } from "node:path"
import { mkdtemp, rm, stat } from "node:fs/promises"
import { tmpdir } from "node:os"
import { existsSync } from "node:fs"
import { resolveStorage } from "./storage"
import { sha256File } from "../core/checksum"
import type { Manifest } from "../core/manifest"

export async function loadManifest(backupId: string, from: string, tempDir: string): Promise<Manifest> {
  const { backend } = resolveStorage(from)
  const manifestFile = join(tempDir, "manifest.json")
  await backend.downloadFile(`${backupId}/manifest.json`, manifestFile)
  return JSON.parse(await Bun.file(manifestFile).text()) as Manifest
}

export async function verifyBackup(options: { backupId: string; from: string }): Promise<{ ok: boolean; errors: string[] }> {
  const { backend } = resolveStorage(options.from)
  const tempDir = await mkdtemp(join(tmpdir(), "securebackup-verify-"))
  const errors: string[] = []

  try {
    let manifest: Manifest
    try {
      manifest = await loadManifest(options.backupId, options.from, tempDir)
    } catch (error) {
      return { ok: false, errors: [`Missing manifest: ${(error as Error).message}`] }
    }

    for (const chunk of manifest.chunks) {
      const remotePath = `${options.backupId}/chunks/${chunk.name}`
      if (!(await backend.exists(remotePath))) {
        errors.push(`Missing chunk: ${chunk.name}`)
        continue
      }
      const localPath = join(tempDir, chunk.name)
      await backend.downloadFile(remotePath, localPath)
      const info = await stat(localPath)
      if (info.size !== chunk.size) errors.push(`Size mismatch for ${chunk.name}: expected ${chunk.size}, got ${info.size}`)
      const hash = await sha256File(localPath)
      if (hash !== chunk.sha256) errors.push(`Checksum mismatch for ${chunk.name}: expected ${chunk.sha256}, got ${hash}`)
    }

    return { ok: errors.length === 0, errors }
  } finally {
    await rm(tempDir, { recursive: true, force: true })
  }
}
