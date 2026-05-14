import { mkdtemp, rm, stat } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { encryptFile } from "../core/encrypt"
import { splitFileToChunks } from "../core/chunk"

export type EncryptDirectOptions = {
  inputFile: string
  recipient: string
  outputFile?: string
  chunksDir?: string
  chunkSize?: number
}

export type EncryptDirectResult = {
  outputFile?: string
  chunksDir?: string
  bytes?: number
  chunks?: number
}

export async function encryptDirect(options: EncryptDirectOptions): Promise<EncryptDirectResult> {
  if (options.chunksDir) {
    const tempDir = await mkdtemp(join(tmpdir(), "securebackup-direct-encrypt-"))
    const encryptedFile = join(tempDir, "payload.age")

    try {
      await encryptFile(options.inputFile, encryptedFile, options.recipient)
      const { chunks } = await splitFileToChunks(encryptedFile, options.chunksDir, options.chunkSize ?? 20 * 1024 * 1024)
      return { chunksDir: options.chunksDir, chunks: chunks.length }
    } finally {
      await rm(tempDir, { recursive: true, force: true })
    }
  }

  if (!options.outputFile) throw new Error("encrypt requires outputFile or chunksDir")
  await encryptFile(options.inputFile, options.outputFile, options.recipient)
  const info = await stat(options.outputFile)
  return { outputFile: options.outputFile, bytes: info.size }
}
