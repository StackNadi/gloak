import { describe, expect, test } from "bun:test"
import * as age from "age-encryption"
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises"
import { existsSync } from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { LocalStorageBackend } from "../src/storage/local"
import { parseSize } from "../src/core/paths"
import { uploadBackup } from "../src/commands/upload"
import { restoreBackup } from "../src/commands/restore"
import { verifyBackup } from "../src/commands/verify"
import { generateKeypair } from "../src/commands/keygen"
import { decryptManifestFromFile } from "../src/core/manifest-crypto"

async function writeEncryptedManifestUnchecked(path: string, manifest: unknown, recipient: string): Promise<void> {
  const encrypter = new age.Encrypter()
  encrypter.addRecipient(recipient)
  await writeFile(path, await encrypter.encrypt(new TextEncoder().encode(JSON.stringify(manifest, null, 2))), { mode: 0o600 })
}

async function makeBackup(dir: string, fileName = "backup.tar") {
  const input = join(dir, fileName)
  const storageRoot = join(dir, "remote")
  await writeFile(input, "secret backup payload")
  const identity = await age.generateIdentity()
  const recipient = await age.identityToRecipient(identity)
  const uploaded = await uploadBackup({ inputFile: input, recipient, to: `local:${storageRoot}`, chunkSize: 8 })
  const manifestPath = join(storageRoot, uploaded.backupId, "manifest.age")
  const manifest = await decryptManifestFromFile(manifestPath, identity, uploaded.backupId)
  return { input, storageRoot, identity, recipient, uploaded, manifestPath, manifest }
}

describe("security hardening", () => {
  test("local backend rejects remote paths that escape the storage root", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-local-path-"))
    try {
      const root = join(dir, "root")
      const outside = join(dir, "outside.txt")
      const source = join(dir, "source.txt")
      await writeFile(source, "nope")
      const backend = new LocalStorageBackend(root)

      await expect(backend.uploadFile(source, "../outside.txt")).rejects.toThrow(/outside storage root|unsafe remote path/i)
      expect(existsSync(outside)).toBe(false)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify rejects traversal backup IDs before reading storage", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-bad-id-"))
    try {
      const verified = await verifyBackup({ backupId: "../../etc", from: `local:${join(dir, "remote")}` })
      expect(verified.ok).toBe(false)
      expect(verified.errors.join("\n")).toMatch(/backup id/i)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify rejects malformed encrypted manifest chunk metadata", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-bad-manifest-"))
    try {
      const { storageRoot, identity, recipient, uploaded, manifestPath, manifest } = await makeBackup(dir)
      manifest.chunks[0].name = "../evil.chunk"
      await writeEncryptedManifestUnchecked(manifestPath, manifest, recipient)

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity })
      expect(verified.ok).toBe(false)
      expect(verified.errors.join("\n")).toMatch(/chunk.*name|manifest/i)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("verify rejects duplicate and non-contiguous encrypted manifest metadata", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-duplicate-manifest-"))
    try {
      const { storageRoot, identity, recipient, uploaded, manifestPath, manifest } = await makeBackup(dir)
      manifest.chunks.push({ ...manifest.chunks[0] })
      manifest.chunking.total_chunks = manifest.chunks.length
      await writeEncryptedManifestUnchecked(manifestPath, manifest, recipient)

      const verified = await verifyBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity })
      expect(verified.ok).toBe(false)
      expect(verified.errors.join("\n")).toMatch(/duplicate|contiguous|index/i)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("restore refuses decrypted manifest filenames that would escape output directory", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-restore-path-"))
    try {
      const { storageRoot, identity, recipient, uploaded, manifestPath, manifest } = await makeBackup(dir)
      const outputDir = join(dir, "restore")
      const escaped = join(dir, "escaped.txt")
      await mkdir(outputDir)
      manifest.source.original_filename = "../escaped.txt"
      await writeEncryptedManifestUnchecked(manifestPath, manifest, recipient)

      await expect(restoreBackup({ backupId: uploaded.backupId, from: `local:${storageRoot}`, identity, output: outputDir })).rejects.toThrow(/filename|manifest/i)
      expect(existsSync(escaped)).toBe(false)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("parseSize rejects unsafe chunk sizes before allocation", () => {
    expect(() => parseSize("63KiB")).toThrow(/chunk size|size/i)
    expect(() => parseSize("2GiB")).toThrow(/chunk size|size/i)
    expect(parseSize("64KiB")).toBe(64 * 1024)
    expect(parseSize("1GiB")).toBe(1024 * 1024 * 1024)
  })

  test("chunking uses bounded zero-filled buffers", async () => {
    const source = await readFile(join(import.meta.dir, "..", "src", "core", "chunk.ts"), "utf8")
    expect(source).not.toContain("Buffer.allocUnsafe")
    expect(source).toContain("Buffer.alloc")
  })

  test("local backend creates private backup directories and files", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-perms-"))
    try {
      const { storageRoot, uploaded, manifest } = await makeBackup(dir)
      const backupDirMode = (await stat(join(storageRoot, uploaded.backupId))).mode & 0o777
      const chunksDirMode = (await stat(join(storageRoot, uploaded.backupId, "chunks"))).mode & 0o777
      const manifestMode = (await stat(join(storageRoot, uploaded.backupId, "manifest.age"))).mode & 0o777
      const locatorMode = (await stat(join(storageRoot, uploaded.backupId, "locator.json"))).mode & 0o777
      const chunkMode = (await stat(join(storageRoot, uploaded.backupId, "chunks", manifest.chunks[0].name))).mode & 0o777

      expect(backupDirMode).toBe(0o700)
      expect(chunksDirMode).toBe(0o700)
      expect(manifestMode).toBe(0o600)
      expect(locatorMode).toBe(0o600)
      expect(chunkMode).toBe(0o600)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("generateKeypair does not return private identity material", async () => {
    const dir = await mkdtemp(join(tmpdir(), "securebackup-no-key-return-"))
    try {
      const result = await generateKeypair({ outputDir: dir })
      expect("identity" in result).toBe(false)
      expect(result.recipient).toStartWith("age1")
      expect(await readFile(join(dir, "identity.txt"), "utf8")).toContain("AGE-SECRET-KEY-")
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })

  test("direct decrypt can require a manifest for chunk integrity verification", async () => {
    const decryptSource = await readFile(join(import.meta.dir, "..", "src", "commands", "decrypt.ts"), "utf8")
    const cliSource = await readFile(join(import.meta.dir, "..", "src", "cli.ts"), "utf8")
    expect(decryptSource).toContain("manifestFile")
    expect(cliSource).toContain('"manifest"')
  })
})
