import { mkdir, open, writeFile } from "node:fs/promises"
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

export async function splitFileToChunks(inputFile: string, chunksDir: string, chunkSize: number): Promise<{ chunks: ChunkMetadata[] }> {
  await mkdir(chunksDir, { recursive: true })
  const input = await open(inputFile, "r")
  const chunks: ChunkMetadata[] = []
  let offset = 0
  let index = 0

  try {
    while (true) {
      const buffer = Buffer.allocUnsafe(chunkSize)
      const { bytesRead } = await input.read(buffer, 0, chunkSize, offset)
      if (bytesRead === 0) break
      const bytes = buffer.subarray(0, bytesRead)
      const name = chunkName(index)
      await writeFile(join(chunksDir, name), bytes, { mode: 0o600 })
      chunks.push({ index, name, size: bytesRead, sha256: sha256Bytes(bytes) })
      offset += bytesRead
      index++
    }
  } finally {
    await input.close()
  }

  return { chunks }
}

export async function concatenateChunks(chunks: ChunkMetadata[], chunksDir: string, outputFile: string): Promise<void> {
  const ordered = [...chunks].sort((a, b) => a.index - b.index)
  const output = createWriteStream(outputFile, { mode: 0o600 })

  for (const chunk of ordered) {
    await new Promise<void>((resolve, reject) => {
      createReadStream(join(chunksDir, chunk.name))
        .on("error", reject)
        .on("end", resolve)
        .pipe(output, { end: false })
    })
  }

  await new Promise<void>((resolve, reject) => output.end((error: Error | null | undefined) => error ? reject(error) : resolve()))
}
