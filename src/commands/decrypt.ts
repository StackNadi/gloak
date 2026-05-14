import { mkdtemp, readdir, rm, stat } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { decryptFile } from "../core/decrypt"
import { concatenateChunks, type ChunkMetadata } from "../core/chunk"

export type DecryptDirectOptions = {
  inputFile?: string
  chunksDir?: string
  outputFile: string
  identity: string
}

function chunkIndex(name: string): number {
  return Number.parseInt(name.replace(/\.chunk$/, ""), 10)
}

export async function decryptDirect(options: DecryptDirectOptions): Promise<{ outputFile: string; bytes: number }> {
  if (options.chunksDir) {
    const tempDir = await mkdtemp(join(tmpdir(), "securebackup-direct-decrypt-"))
    const encryptedFile = join(tempDir, "payload.age")

    try {
      const names = (await readdir(options.chunksDir))
        .filter((name) => /^\d{6}\.chunk$/.test(name))
        .sort()
      if (names.length === 0) throw new Error(`No chunk files found in ${options.chunksDir}`)

      const chunks: ChunkMetadata[] = await Promise.all(names.map(async (name) => {
        const info = await stat(join(options.chunksDir!, name))
        return { index: chunkIndex(name), name, size: info.size, sha256: "" }
      }))

      await concatenateChunks(chunks, options.chunksDir, encryptedFile)
      await decryptFile(encryptedFile, options.outputFile, options.identity)
    } finally {
      await rm(tempDir, { recursive: true, force: true })
    }
  } else {
    if (!options.inputFile) throw new Error("decrypt requires inputFile or chunksDir")
    await decryptFile(options.inputFile, options.outputFile, options.identity)
  }

  const info = await stat(options.outputFile)
  return { outputFile: options.outputFile, bytes: info.size }
}
