import type { ChunkMetadata } from "./chunk"
import { chunkName } from "./paths"
import { isValidBackupId } from "./ids"

export type Manifest = {
  version: 1
  backup_id: string
  created_at: string
  app: { name: "securebackup"; version: string; runtime: "bun" }
  source: { original_filename: string; original_size: number }
  encryption: { format: "age"; library: "age-encryption"; mode: "recipient"; recipient: string }
  chunking: { chunk_size: number; total_chunks: number }
  integrity: { encrypted_file_sha256: string }
  storage: { backend: string; layout: "filesystem-v1" }
  chunks: ChunkMetadata[]
}

export type ManifestV2 = {
  version: 2
  backup_id: string
  created_at: string
  app: { name: "securebackup"; version: string; runtime: "bun" }
  source: { original_filename: string; original_size: number }
  payload: {
    encryption: { format: "age"; library: "age-encryption"; mode: "recipient"; recipients: string[] }
    encrypted_file_sha256: string
  }
  manifest_encryption: { format: "age"; library: "age-encryption"; mode: "recipient"; recipients: string[] }
  chunking: { chunk_size: number; total_chunks: number }
  storage: { backend: string; layout: "filesystem-v2" }
  chunks: ChunkMetadata[]
}

const SHA256_HEX = /^[a-f0-9]{64}$/
const CHUNK_NAME = /^\d{6}\.chunk$/

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function isSafeRestoreFilename(value: unknown): value is string {
  if (typeof value !== "string" || value.length === 0) return false
  if (value === "." || value === "..") return false
  if (value.includes("\0") || value.includes("/") || value.includes("\\")) return false
  if (/^[a-zA-Z]:/.test(value)) return false
  return true
}

export function safeRestoreFilename(value: string): string {
  if (!isSafeRestoreFilename(value)) {
    throw new Error(`Invalid restore filename in manifest: ${JSON.stringify(value)}`)
  }
  return value
}

function validateCommonManifestFields(manifest: Record<string, unknown>, requestedBackupId: string | undefined, errors: string[]): void {
  if (typeof manifest.backup_id !== "string" || !isValidBackupId(manifest.backup_id)) errors.push("Manifest backup_id must be a UUID")
  if (requestedBackupId !== undefined && manifest.backup_id !== requestedBackupId) errors.push("Manifest backup_id does not match requested backup ID")
  if (typeof manifest.created_at !== "string" || Number.isNaN(Date.parse(manifest.created_at))) errors.push("Manifest created_at must be an ISO timestamp")

  const app = manifest.app
  if (!isRecord(app) || app.name !== "securebackup" || typeof app.version !== "string" || app.runtime !== "bun") {
    errors.push("Manifest app metadata is invalid")
  }

  const source = manifest.source
  if (!isRecord(source)) {
    errors.push("Manifest source metadata is invalid")
  } else {
    if (!isSafeRestoreFilename(source.original_filename)) errors.push("Manifest source.original_filename is unsafe")
    if (!Number.isSafeInteger(source.original_size) || source.original_size < 0) errors.push("Manifest source.original_size must be a non-negative safe integer")
  }

  const chunking = manifest.chunking
  if (!isRecord(chunking)) {
    errors.push("Manifest chunking metadata is invalid")
  } else {
    if (!Number.isSafeInteger(chunking.chunk_size) || chunking.chunk_size <= 0) errors.push("Manifest chunk_size must be a positive safe integer")
    if (!Number.isSafeInteger(chunking.total_chunks) || chunking.total_chunks < 0) errors.push("Manifest total_chunks must be a non-negative safe integer")
  }

  if (!Array.isArray(manifest.chunks)) {
    errors.push("Manifest chunks must be an array")
  } else {
    const totalChunks = isRecord(chunking) && Number.isSafeInteger(chunking.total_chunks) ? chunking.total_chunks : undefined
    if (totalChunks !== undefined && manifest.chunks.length !== totalChunks) {
      errors.push("Manifest chunking.total_chunks does not match chunks length")
    }

    const names = new Set<string>()
    const indexes = new Set<number>()
    manifest.chunks.forEach((chunk, position) => {
      if (!isRecord(chunk)) {
        errors.push(`Manifest chunk ${position} must be an object`)
        return
      }

      const index = chunk.index
      const name = chunk.name
      if (!Number.isSafeInteger(index) || index < 0) {
        errors.push(`Manifest chunk ${position} index must be a non-negative integer`)
      } else {
        if (indexes.has(index)) errors.push(`Manifest contains duplicate chunk index ${index}`)
        indexes.add(index)
        if (name !== chunkName(index)) errors.push(`Manifest chunk ${position} name must match index ${index}`)
      }

      if (typeof name !== "string" || !CHUNK_NAME.test(name)) {
        errors.push(`Manifest chunk ${position} name is invalid`)
      } else {
        if (names.has(name)) errors.push(`Manifest contains duplicate chunk name ${name}`)
        names.add(name)
      }

      if (!Number.isSafeInteger(chunk.size) || chunk.size < 0) errors.push(`Manifest chunk ${position} size must be a non-negative safe integer`)
      if (typeof chunk.sha256 !== "string" || !SHA256_HEX.test(chunk.sha256)) errors.push(`Manifest chunk ${position} sha256 must be lowercase SHA-256 hex`)
    })

    for (let index = 0; index < manifest.chunks.length; index++) {
      if (!indexes.has(index)) errors.push(`Manifest chunk indexes must be contiguous from zero; missing ${index}`)
    }
  }
}

export function validateManifest(manifest: unknown, requestedBackupId?: string): { ok: boolean; errors: string[]; manifest?: Manifest } {
  const errors: string[] = []

  if (!isRecord(manifest)) {
    return { ok: false, errors: ["Manifest must be a JSON object"] }
  }

  if (manifest.version !== 1) errors.push("Manifest version must be 1")
  validateCommonManifestFields(manifest, requestedBackupId, errors)

  const encryption = manifest.encryption
  if (!isRecord(encryption) || encryption.format !== "age" || encryption.library !== "age-encryption" || encryption.mode !== "recipient" || typeof encryption.recipient !== "string" || !encryption.recipient.startsWith("age1")) {
    errors.push("Manifest encryption metadata is invalid")
  }

  const integrity = manifest.integrity
  if (!isRecord(integrity) || typeof integrity.encrypted_file_sha256 !== "string" || !SHA256_HEX.test(integrity.encrypted_file_sha256)) {
    errors.push("Manifest encrypted_file_sha256 must be lowercase SHA-256 hex")
  }

  const storage = manifest.storage
  if (!isRecord(storage) || typeof storage.backend !== "string" || storage.layout !== "filesystem-v1") {
    errors.push("Manifest storage metadata is invalid")
  }

  return errors.length === 0 ? { ok: true, errors, manifest: manifest as Manifest } : { ok: false, errors }
}

function validateRecipientList(value: unknown, field: string, errors: string[]): void {
  if (!Array.isArray(value) || value.length === 0 || value.some((recipient) => typeof recipient !== "string" || !recipient.startsWith("age1"))) {
    errors.push(`Manifest ${field} recipients are invalid`)
  }
}

export function validateManifestV2(manifest: unknown, requestedBackupId?: string): { ok: boolean; errors: string[]; manifest?: ManifestV2 } {
  const errors: string[] = []

  if (!isRecord(manifest)) {
    return { ok: false, errors: ["Manifest must be a JSON object"] }
  }

  if (manifest.version !== 2) errors.push("Manifest version must be 2")
  validateCommonManifestFields(manifest, requestedBackupId, errors)

  const payload = manifest.payload
  if (!isRecord(payload)) {
    errors.push("Manifest payload metadata is invalid")
  } else {
    const encryption = payload.encryption
    if (!isRecord(encryption) || encryption.format !== "age" || encryption.library !== "age-encryption" || encryption.mode !== "recipient") {
      errors.push("Manifest payload encryption metadata is invalid")
    } else {
      validateRecipientList(encryption.recipients, "payload encryption", errors)
    }
    if (typeof payload.encrypted_file_sha256 !== "string" || !SHA256_HEX.test(payload.encrypted_file_sha256)) {
      errors.push("Manifest payload encrypted_file_sha256 must be lowercase SHA-256 hex")
    }
  }

  const manifestEncryption = manifest.manifest_encryption
  if (!isRecord(manifestEncryption) || manifestEncryption.format !== "age" || manifestEncryption.library !== "age-encryption" || manifestEncryption.mode !== "recipient") {
    errors.push("Manifest encryption metadata is invalid")
  } else {
    validateRecipientList(manifestEncryption.recipients, "manifest encryption", errors)
  }

  const storage = manifest.storage
  if (!isRecord(storage) || typeof storage.backend !== "string" || storage.layout !== "filesystem-v2") {
    errors.push("Manifest storage metadata is invalid")
  }

  return errors.length === 0 ? { ok: true, errors, manifest: manifest as ManifestV2 } : { ok: false, errors }
}
