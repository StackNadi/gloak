import { join, relative, resolve } from "node:path"
import { mkdtemp, mkdir, rm, stat } from "node:fs/promises"
import { existsSync } from "node:fs"
import { tmpdir } from "node:os"
import { resolveStorage } from "./storage"
import { loadManifest } from "./verify"
import { concatenateChunks } from "../core/chunk"
import { sha256File } from "../core/checksum"
import { decryptFile } from "../core/decrypt"
import { safeRestoreFilename } from "../core/manifest"
import { createProgressBar } from "../core/progress"

function assertInsideDirectory(parentDir: string, childPath: string): void {
  const parent = resolve(parentDir)
  const child = resolve(childPath)
  const rel = relative(parent, child)
  if (rel === ".." || rel.startsWith("../") || rel.startsWith("..\\") || resolve(rel) === rel) {
    throw new Error("Restore output escaped target directory")
  }
}

export async function restoreBackup(options: { backupId: string; from: string; identity: string; output: string }): Promise<{ outputFile: string }> {
  const { backend } = resolveStorage(options.from)
  const tempDir = await mkdtemp(join(tmpdir(), "securebackup-restore-"))
  const chunksDir = join(tempDir, "chunks")
  const encryptedFile = join(tempDir, "payload.age")

  try {
    // ── Validate locator + decrypt manifest (lightweight, no chunk download) ──
    await mkdir(chunksDir, { recursive: true, mode: 0o700 })
    const manifest = await loadManifest(options.backupId, options.from, tempDir, options.identity)

    // ── Download chunks ──────────────────────────────────────────
    const downloadBar = createProgressBar({ total: manifest.chunks.length, label: "Downloading chunks" })
    for (const chunk of manifest.chunks) {
      await backend.downloadFile(`${options.backupId}/chunks/${chunk.name}`, join(chunksDir, chunk.name))
      downloadBar.increment()
    }
    downloadBar.stop()

    // ── Verify each chunk against manifest metadata ──────────────
    const verifyBar = createProgressBar({ total: manifest.chunks.length, label: "Verifying chunks" })
    for (const chunk of manifest.chunks) {
      const localPath = join(chunksDir, chunk.name)
      const info = await stat(localPath).catch(() => null)
      if (!info) throw new Error(`Missing chunk after download: ${chunk.name}`)
      if (info.size !== chunk.size) throw new Error(`Size mismatch for ${chunk.name}: expected ${chunk.size}, got ${info.size}`)
      const hash = await sha256File(localPath)
      if (hash !== chunk.sha256) throw new Error(`Checksum mismatch for ${chunk.name}: expected ${chunk.sha256}, got ${hash}`)
      verifyBar.increment()
    }
    verifyBar.stop()

    // ── Join chunks into encrypted file ──────────────────────────
    const joinBar = createProgressBar({ total: manifest.chunks.length, label: "Joining chunks" })
    await concatenateChunks(manifest.chunks, chunksDir, encryptedFile, (c) => joinBar.update(c))
    joinBar.stop()

    // ── Final integrity check ────────────────────────────────────
    const encryptedHash = await sha256File(encryptedFile)
    if (encryptedHash !== manifest.payload.encrypted_file_sha256) {
      throw new Error(`Encrypted file checksum mismatch: expected ${manifest.payload.encrypted_file_sha256}, got ${encryptedHash}`)
    }

    // ── Decrypt ──────────────────────────────────────────────────
    let outputFile = options.output
    if (options.output.endsWith("/") || (existsSync(options.output) && (await stat(options.output)).isDirectory())) {
      await mkdir(options.output, { recursive: true, mode: 0o700 })
      const safeName = safeRestoreFilename(manifest.source.original_filename)
      outputFile = join(options.output, safeName)
      assertInsideDirectory(options.output, outputFile)
    }
    await decryptFile(encryptedFile, outputFile, options.identity)

    return { outputFile }
  } finally {
    await rm(tempDir, { recursive: true, force: true })
  }
}
