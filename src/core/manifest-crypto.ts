import * as age from "age-encryption"
import { readFile, writeFile } from "node:fs/promises"
import { validateManifestV2, type ManifestV2 } from "./manifest"

function stableJson(value: unknown): string {
  return JSON.stringify(value, Object.keys(value as Record<string, unknown>).sort(), 2)
}

export async function encryptManifestToFile(manifest: ManifestV2, outputFile: string, recipient: string): Promise<void> {
  const validated = validateManifestV2(manifest, manifest.backup_id)
  if (!validated.ok) throw new Error(`Invalid manifest before encryption: ${validated.errors.join("; ")}`)

  const encrypter = new age.Encrypter()
  encrypter.addRecipient(recipient)
  const plaintext = new TextEncoder().encode(JSON.stringify(manifest, null, 2))
  const ciphertext = await encrypter.encrypt(plaintext)
  await writeFile(outputFile, ciphertext, { mode: 0o600 })
}

export async function decryptManifestFromFile(inputFile: string, identity: string, requestedBackupId?: string): Promise<ManifestV2> {
  const ciphertext = await readFile(inputFile)
  const decrypter = new age.Decrypter()
  decrypter.addIdentity(identity)
  const plaintext = await decrypter.decrypt(ciphertext, "uint8array")
  const parsed = JSON.parse(new TextDecoder().decode(plaintext))
  const validated = validateManifestV2(parsed, requestedBackupId)
  if (!validated.ok || !validated.manifest) throw new Error(`Invalid encrypted manifest: ${validated.errors.join("; ")}`)
  return validated.manifest
}
