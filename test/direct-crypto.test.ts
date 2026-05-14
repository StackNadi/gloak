import { describe, expect, test } from "bun:test"
import * as age from "age-encryption"
import { mkdtemp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { encryptDirect } from "../src/commands/encrypt"
import { decryptDirect } from "../src/commands/decrypt"

describe("direct encrypt/decrypt commands", () => {
  test("encrypts and decrypts a file without chunking or manifest", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-direct-"))
    try {
      const input = join(dir, "secret.txt")
      const encrypted = join(dir, "secret.txt.age")
      const restored = join(dir, "restored.txt")
      await writeFile(input, "direct encrypted payload")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)

      const encryptedResult = await encryptDirect({ inputFile: input, outputFile: encrypted, recipient })
      expect(encryptedResult.outputFile).toBe(encrypted)
      expect(encryptedResult.bytes).toBe((await stat(encrypted)).size)
      expect(await readFile(encrypted, "utf8")).not.toContain("direct encrypted payload")

      const decryptedResult = await decryptDirect({ inputFile: encrypted, outputFile: restored, identity })
      expect(decryptedResult.outputFile).toBe(restored)
      expect(decryptedResult.bytes).toBe((await stat(restored)).size)
      expect(await readFile(restored, "utf8")).toBe("direct encrypted payload")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("can split the encrypted output into chunks without creating a backup manifest", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-direct-chunks-"))
    try {
      const input = join(dir, "secret.txt")
      const chunksDir = join(dir, "chunks")
      await writeFile(input, "this gets encrypted first, then split into chunks")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)

      const result = await encryptDirect({
        inputFile: input,
        recipient,
        chunksDir,
        chunkSize: 12,
      })

      expect(result.outputFile).toBeUndefined()
      expect(result.chunksDir).toBe(chunksDir)
      expect(result.chunks).toBeGreaterThan(1)
      expect((await readdir(chunksDir)).sort()).toEqual(
        Array.from({ length: result.chunks }, (_, index) => `${String(index).padStart(6, "0")}.chunk`),
      )
      expect(await Bun.file(join(chunksDir, "manifest.json")).exists()).toBe(false)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})
