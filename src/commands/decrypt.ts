import { mkdtemp, readdir, readFile, rm, stat } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { decryptFile } from "../core/decrypt"
import { concatenateChunks, type ChunkMetadata } from "../core/chunk"
import { sha256File } from "../core/checksum"
import { validateManifest, type Manifest } from "../core/manifest"
import { createProgressBar } from "../core/progress"
import { consola } from "consola"

export type DecryptDirectOptions = {
  inputFile?: string
  chunksDir?: string
  manifestFile?: string
  outputFile: string
  identity: string
}

function chunkIndex(name: string): number {
  return Number.parseInt(name.replace(/\.chunk$/, ""), 10)
}

async function loadDirectManifest(manifestFile: string): Promise<Manifest> {
  const parsed = JSON.parse(await readFile(manifestFile, "utf8"))
  const validated = validateManifest(parsed)
  if (!validated.ok || !validated.manifest) throw new Error(`Invalid manifest: ${validated.errors.join("; ")}`)
  return validated.manifest
}

async function chunksFromManifest(chunksDir: string, manifestFile: string): Promise<ChunkMetadata[]> {
  const manifest = await loadDirectManifest(manifestFile)
  const bar = createProgressBar({ total: manifest.chunks.length, label: "Verifying chunks" })
  for (const chunk of manifest.chunks) {
    const chunkPath = join(chunksDir, chunk.name)
    const info = await stat(chunkPath)
    if (info.size !== chunk.size) throw new Error(`Size mismatch for ${chunk.name}: expected ${chunk.size}, got ${info.size}`)
    const hash = await sha256File(chunkPath)
    if (hash !== chunk.sha256) throw new Error(`Checksum mismatch for ${chunk.name}: expected ${chunk.sha256}, got ${hash}`)
    bar.increment()
  }
  bar.stop()
  return manifest.chunks
}

async function chunksFromDirectoryUnsafe(chunksDir: string): Promise<ChunkMetadata[]> {
  const names = (await readdir(chunksDir))
    .filter((name) => /^\d{6}\.chunk$/.test(name))
    .sort()
  if (names.length === 0) throw new Error(`No chunk files found in ${chunksDir}`)

  return Promise.all(names.map(async (name) => {
    const info = await stat(join(chunksDir, name))
    return { index: chunkIndex(name), name, size: info.size, sha256: "" }
  }))
}

export async function decryptDirect(options: DecryptDirectOptions): Promise<{ outputFile: string; bytes: number }> {
  if (options.chunksDir) {
    const tempDir = await mkdtemp(join(tmpdir(), "securebackup-direct-decrypt-"))
    const encryptedFile = join(tempDir, "payload.age")

    try {
      if (!options.manifestFile) throw new Error("decrypt --chunks-dir requires --manifest for chunk integrity verification")
      const chunks = options.manifestFile
        ? await chunksFromManifest(options.chunksDir, options.manifestFile)
        : await chunksFromDirectoryUnsafe(options.chunksDir)

      const bar = createProgressBar({ total: chunks.length, label: "Joining chunks" })
      await concatenateChunks(chunks, options.chunksDir, encryptedFile, (c) => bar.update(c))
      if (options.manifestFile) {
        const manifest = await loadDirectManifest(options.manifestFile)
        const encryptedHash = await sha256File(encryptedFile)
        if (encryptedHash !== manifest.integrity.encrypted_file_sha256) {
          throw new Error(`Encrypted file checksum mismatch: expected ${manifest.integrity.encrypted_file_sha256}, got ${encryptedHash}`)
        }
      }
      consola.info("Decrypting...")
      await decryptFile(encryptedFile, options.outputFile, options.identity)
    } finally {
      await rm(tempDir, { recursive: true, force: true })
    }
  } else {
    if (!options.inputFile) throw new Error("decrypt requires inputFile or chunksDir")
    const inputSize = (await stat(options.inputFile)).size
    consola.info(`Decrypting ${(inputSize / 1024 / 1024).toFixed(1)} MiB...`)
    await decryptFile(options.inputFile, options.outputFile, options.identity)
  }

  const info = await stat(options.outputFile)
  consola.success(`Decrypted to ${options.outputFile} (${(info.size / 1024 / 1024).toFixed(1)} MiB)`)
  return { outputFile: options.outputFile, bytes: info.size }
}
