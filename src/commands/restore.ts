import { basename, join } from "node:path"
import { mkdtemp, mkdir, rm, stat } from "node:fs/promises"
import { existsSync } from "node:fs"
import { tmpdir } from "node:os"
import { resolveStorage } from "./storage"
import { verifyBackup, loadManifest } from "./verify"
import { concatenateChunks } from "../core/chunk"
import { sha256File } from "../core/checksum"
import { decryptFile } from "../core/decrypt"

export async function restoreBackup(options: { backupId: string; from: string; identity: string; output: string }): Promise<{ outputFile: string }> {
  const verified = await verifyBackup({ backupId: options.backupId, from: options.from })
  if (!verified.ok) throw new Error(`Backup verification failed: ${verified.errors.join("; ")}`)

  const { backend } = resolveStorage(options.from)
  const tempDir = await mkdtemp(join(tmpdir(), "securebackup-restore-"))
  const chunksDir = join(tempDir, "chunks")
  const encryptedFile = join(tempDir, "payload.age")

  try {
    await mkdir(chunksDir, { recursive: true })
    const manifest = await loadManifest(options.backupId, options.from, tempDir)
    for (const chunk of manifest.chunks) {
      await backend.downloadFile(`${options.backupId}/chunks/${chunk.name}`, join(chunksDir, chunk.name))
    }
    await concatenateChunks(manifest.chunks, chunksDir, encryptedFile)
    const encryptedHash = await sha256File(encryptedFile)
    if (encryptedHash !== manifest.integrity.encrypted_file_sha256) {
      throw new Error(`Encrypted file checksum mismatch: expected ${manifest.integrity.encrypted_file_sha256}, got ${encryptedHash}`)
    }

    let outputFile = options.output
    if (options.output.endsWith("/") || (existsSync(options.output) && (await stat(options.output)).isDirectory())) {
      await mkdir(options.output, { recursive: true })
      outputFile = join(options.output, manifest.source.original_filename)
    }
    await decryptFile(encryptedFile, outputFile, options.identity)
    return { outputFile }
  } finally {
    await rm(tempDir, { recursive: true, force: true })
  }
}
