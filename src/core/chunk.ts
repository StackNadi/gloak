import { mkdir, open, stat, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { createReadStream, createWriteStream } from "node:fs"
import { chunkName } from "./paths"
import { sha256Bytes } from "./checksum"

export type ChunkMetadata = {
  index: number
  name: string
  size: number
  sha256: string
}

export async function splitFileToChunks(
  inputFile: string,
  chunksDir: string,
  chunkSize: number,
  onProgress?: (current: number, total: number) => void,
): Promise<{ chunks: ChunkMetadata[] }> {
  await mkdir(chunksDir, { recursive: true })
  const input = await open(inputFile, "r")
  const inputSize = (await stat(inputFile)).size
  const estimatedTotal = Math.ceil(inputSize / chunkSize)
  const chunks: ChunkMetadata[] = []
  let offset = 0
  let index = 0

  try {
    while (true) {
      const buffer = Buffer.alloc(chunkSize)
      const { bytesRead } = await input.read(buffer, 0, chunkSize, offset)
      if (bytesRead === 0) break
      const bytes = buffer.subarray(0, bytesRead)
      const name = chunkName(index)
      await writeFile(join(chunksDir, name), bytes, { mode: 0o600 })
      chunks.push({ index, name, size: bytesRead, sha256: sha256Bytes(bytes) })
      offset += bytesRead
      index++
      onProgress?.(index, estimatedTotal)
    }
  } finally {
    await input.close()
  }

  return { chunks }
}

export async function concatenateChunks(
  chunks: ChunkMetadata[],
  chunksDir: string,
  outputFile: string,
  onProgress?: (current: number, total: number) => void,
): Promise<void> {
  const ordered = [...chunks].sort((a, b) => a.index - b.index)
  const output = createWriteStream(outputFile, { mode: 0o600 })

  for (const [index, chunk] of ordered.entries()) {
    await new Promise<void>((resolve, reject) => {
      createReadStream(join(chunksDir, chunk.name))
        .on("error", reject)
        .on("end", resolve)
        .pipe(output, { end: false })
    })
    onProgress?.(index + 1, ordered.length)
  }

  await new Promise<void>((resolve, reject) => output.end((error: Error | null | undefined) => error ? reject(error) : resolve()))
}
