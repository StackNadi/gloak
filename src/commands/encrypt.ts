import { mkdtemp, rm, stat } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { encryptFile } from "../core/encrypt"
import { splitFileToChunks } from "../core/chunk"
import { createProgressBar } from "../core/progress"
import { consola } from "consola"

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
      consola.info("Encrypting file...")
      await encryptFile(options.inputFile, encryptedFile, options.recipient)
      const encryptedSize = (await stat(encryptedFile)).size
      const totalChunks = Math.ceil(encryptedSize / (options.chunkSize ?? 20 * 1024 * 1024))

      const bar = createProgressBar({ total: totalChunks, label: "Splitting chunks" })
      const { chunks } = await splitFileToChunks(encryptedFile, options.chunksDir, options.chunkSize ?? 20 * 1024 * 1024, (current) => bar.update(current))
      bar.stop()

      return { chunksDir: options.chunksDir, chunks: chunks.length }
    } finally {
      await rm(tempDir, { recursive: true, force: true })
    }
  }

  if (!options.outputFile) throw new Error("encrypt requires outputFile or chunksDir")
  const inputSize = (await stat(options.inputFile)).size
  consola.info(`Encrypting ${(inputSize / 1024 / 1024).toFixed(1)} MiB...`)
  await encryptFile(options.inputFile, options.outputFile, options.recipient)
  const info = await stat(options.outputFile)
  consola.success(`Encrypted to ${options.outputFile} (${(info.size / 1024 / 1024).toFixed(1)} MiB)`)
  return { outputFile: options.outputFile, bytes: info.size }
}
