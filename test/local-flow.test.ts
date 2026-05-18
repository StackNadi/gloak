import { describe, expect, test } from "bun:test"
import * as age from "age-encryption"
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises"
import { existsSync } from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { uploadBackup } from "../src/commands/upload"
import { restoreBackup } from "../src/commands/restore"
import { verifyBackup } from "../src/commands/verify"

describe("local backend upload/verify/restore", () => {
  test("uploads encrypted manifest v2, verifies chunks, and restores original file", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-flow-"))
    try {
      const input = join(dir, "backup.tar")
      const storageRoot = join(dir, "remote")
      const outputDir = join(dir, "restored")
      await mkdir(outputDir)
      await writeFile(input, "hello encrypted cloud backup")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)

      const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })
      expect(uploaded.backupId).toMatch(/^[0-9a-f-]{36}$/)
      expect(uploaded.remote).toBe(`local:${storageRoot}/${uploaded.backupId}`)

      const backupRoot = join(storageRoot, uploaded.backupId)
      const locatorPath = join(backupRoot, "locator.json")
      const manifestPath = join(backupRoot, "manifest.age")
      expect(existsSync(locatorPath)).toBe(true)
      expect(existsSync(manifestPath)).toBe(true)
      expect(existsSync(join(backupRoot, "manifest.json"))).toBe(false)

      const locator = JSON.parse(await readFile(locatorPath, "utf8"))
      expect(locator).toEqual({
        version: 2,
        app: "securebackup",
        backup_id: uploaded.backupId,
        manifest: { name: "manifest.age", encryption: "age" },
        storage: { layout: "filesystem-v2" },
      })
      const locatorText = await readFile(locatorPath, "utf8")
      expect(locatorText).not.toContain("backup.tar")
      expect(locatorText).not.toContain(recipient)
      expect(locatorText).not.toContain("encrypted_file_sha256")
      expect(locatorText).not.toContain("chunks")

      const encryptedManifestText = await readFile(manifestPath, "utf8")
      expect(encryptedManifestText).not.toContain("backup.tar")
      expect(encryptedManifestText).not.toContain(recipient)
      expect(encryptedManifestText).not.toContain("encrypted_file_sha256")
      expect(encryptedManifestText).not.toContain("000000.chunk")

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity })
      expect(verified.ok).toBe(true)

      const restored = await restoreBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity, output: outputDir })
      expect(restored.outputFile).toBe(join(outputDir, "backup.tar"))
      expect(await readFile(restored.outputFile, "utf8")).toBe("hello encrypted cloud backup")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify requires an identity for encrypted manifest v2", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-v2-identity-"))
    try {
      const input = join(dir, "backup.tar")
      const storageRoot = join(dir, "remote")
      await writeFile(input, "hello encrypted cloud backup")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)
      const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}` })
      expect(verified.ok).toBe(false)
      expect(verified.errors.join("\n")).toMatch(/identity/i)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify detects missing chunks after decrypting v2 manifest", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-missing-"))
    try {
      const input = join(dir, "backup.tar")
      const storageRoot = join(dir, "remote")
      await writeFile(input, "hello encrypted cloud backup")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)
      const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })
      await rm(join(storageRoot, uploaded.backupId, "chunks", "000000.chunk"))

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity })
      expect(verified.ok).toBe(false)
      expect(verified.errors[0]).toContain("Missing chunk")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify detects checksum mismatches after decrypting v2 manifest", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-corrupt-"))
    try {
      const input = join(dir, "backup.tar")
      const storageRoot = join(dir, "remote")
      await writeFile(input, "hello encrypted cloud backup")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)
      const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })
      await writeFile(join(storageRoot, uploaded.backupId, "chunks", "000000.chunk"), "corrupt!")

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity })
      expect(verified.ok).toBe(false)
      expect(verified.errors[0]).toContain("Checksum mismatch")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify rejects tampered encrypted manifest before chunk verification", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-tampered-manifest-"))
    try {
      const input = join(dir, "backup.tar")
      const storageRoot = join(dir, "remote")
      await writeFile(input, "hello encrypted cloud backup")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)
      const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })
      await writeFile(join(storageRoot, uploaded.backupId, "manifest.age"), "not an age encrypted manifest")

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity })
      expect(verified.ok).toBe(false)
      expect(verified.errors.join("\n")).toMatch(/manifest|decrypt/i)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})
