import { describe, expect, test } from "bun:test"
import { mkdtemp, readFile, rm, stat } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { generateKeypair } from "../src/commands/keygen"

describe("key generation", () => {
  test("stores identity and recipient under the configured securebackup home", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-keygen-"))
    try {
      const result = await generateKeypair({ homeDir: dir })

      expect(result.keyDir).toBe(join(dir, ".securebackup"))
      expect(result.identityFile).toBe(join(dir, ".securebackup", "identity.txt"))
      expect(result.recipientFile).toBe(join(dir, ".securebackup", "recipient.txt"))
      expect(result.recipient).toStartWith("age1")

      const identityText = await readFile(result.identityFile, "utf8")
      expect(identityText).toContain("AGE-SECRET-KEY-")
      expect(identityText).toContain("# created:")
      expect(identityText).toContain("# public key:")
      expect(identityText).toContain(result.recipient)
      expect(identityText).toContain("do not edit")

      const recipientText = await readFile(result.recipientFile, "utf8")
      expect(recipientText).toContain(result.recipient)
      expect(recipientText).toContain("# created:")
      expect(recipientText).toContain("auto-generated")

      const dirMode = (await stat(result.keyDir)).mode & 0o777
      const identityMode = (await stat(result.identityFile)).mode & 0o777
      const recipientMode = (await stat(result.recipientFile)).mode & 0o777
      expect(dirMode).toBe(0o700)
      expect(identityMode).toBe(0o600)
      expect(recipientMode).toBe(0o644)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("supports custom outputDir directly", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-keygen-"))
    try {
      const result = await generateKeypair({ outputDir: dir })

      expect(result.keyDir).toBe(dir)
      expect(result.identityFile).toBe(join(dir, "identity.txt"))
      expect(result.recipientFile).toBe(join(dir, "recipient.txt"))
      expect(result.recipient).toStartWith("age1")

      const identityText = await readFile(result.identityFile, "utf8")
      expect(identityText).toContain("AGE-SECRET-KEY-")

      const recipientText = await readFile(result.recipientFile, "utf8")
      expect(recipientText).toContain(result.recipient)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})
