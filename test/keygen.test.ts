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
      expect(result.identity).toStartWith("AGE-SECRET-KEY-")
      expect(result.recipient).toStartWith("age1")

      expect(await readFile(result.identityFile, "utf8")).toBe(`${result.identity}\n`)
      expect(await readFile(result.recipientFile, "utf8")).toBe(`${result.recipient}\n`)

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
})
