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
  test("uploads to UUID folder, writes manifest, verifies chunks, and restores original file", async () => {
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

      const manifestPath = join(storageRoot, uploaded.backupId, "manifest.json")
      const manifest = JSON.parse(await readFile(manifestPath, "utf8"))
      expect(manifest.backup_id).toBe(uploaded.backupId)
      expect(manifest.app.runtime).toBe("bun")
      expect(manifest.encryption.library).toBe("age-encryption")
      expect(manifest.source.original_filename).toBe("backup.tar")
      expect(manifest.chunks.every((chunk: { name: string }) => /^\d{6}\.chunk$/.test(chunk.name))).toBe(true)
      expect(existsSync(join(storageRoot, uploaded.backupId, "chunks", manifest.chunks[0].name))).toBe(true)

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}` })
      expect(verified.ok).toBe(true)

      const restored = await restoreBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity, output: outputDir })
      expect(restored.outputFile).toBe(join(outputDir, "backup.tar"))
      expect(await readFile(restored.outputFile, "utf8")).toBe("hello encrypted cloud backup")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify detects missing chunks", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-missing-"))
    try {
      const input = join(dir, "backup.tar")
      const storageRoot = join(dir, "remote")
      await writeFile(input, "hello encrypted cloud backup")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)
      const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })
      const manifest = JSON.parse(await readFile(join(storageRoot, uploaded.backupId, "manifest.json"), "utf8"))
      await rm(join(storageRoot, uploaded.backupId, "chunks", manifest.chunks[0].name))

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}` })
      expect(verified.ok).toBe(false)
      expect(verified.errors[0]).toContain("Missing chunk")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify detects checksum mismatches", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-corrupt-"))
    try {
      const input = join(dir, "backup.tar")
      const storageRoot = join(dir, "remote")
      await writeFile(input, "hello encrypted cloud backup")
      const identity = await age.generateIdentity()
      const recipient = await age.identityToRecipient(identity)
      const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })
      const manifest = JSON.parse(await readFile(join(storageRoot, uploaded.backupId, "manifest.json"), "utf8"))
      await writeFile(join(storageRoot, uploaded.backupId, "chunks", manifest.chunks[0].name), "corrupt!")

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}` })
      expect(verified.ok).toBe(false)
      expect(verified.errors[0]).toContain("Checksum mismatch")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})
