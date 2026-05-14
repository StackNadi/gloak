import { describe, expect, test } from "bun:test"
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { splitFileToChunks, concatenateChunks } from "../src/core/chunk"

describe("chunking", () => {
  test("splits encrypted payload into fixed-width chunk names and restores by index", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-test-"))
    try {
      const input = join(dir, "payload.age")
      const chunksDir = join(dir, "chunks")
      const restored = join(dir, "restored.age")
      await writeFile(input, Buffer.from("abcdefghijklmnopqrstuvwxyz"))

      const result = await splitFileToChunks(input, chunksDir, 10)
      expect(result.chunks.map((chunk) => chunk.name)).toEqual(["000000.chunk", "000001.chunk", "000002.chunk"])
      expect(result.chunks.map((chunk) => chunk.index)).toEqual([0, 1, 2])

      await concatenateChunks(result.chunks, chunksDir, restored)
      expect(await readFile(restored, "utf8")).toBe("abcdefghijklmnopqrstuvwxyz")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})
