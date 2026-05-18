import { isValidBackupId } from "./ids"

export type PublicLocatorV2 = {
  version: 2
  app: "securebackup"
  backup_id: string
  manifest: { name: "manifest.age"; encryption: "age" }
  storage: { layout: "filesystem-v2" }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

export function createLocatorV2(backupId: string): PublicLocatorV2 {
  if (!isValidBackupId(backupId)) throw new Error(`Invalid backup ID: ${backupId}`)
  return {
    version: 2,
    app: "securebackup",
    backup_id: backupId,
    manifest: { name: "manifest.age", encryption: "age" },
    storage: { layout: "filesystem-v2" },
  }
}

export function validateLocatorV2(locator: unknown, requestedBackupId?: string): { ok: boolean; errors: string[]; locator?: PublicLocatorV2 } {
  const errors: string[] = []
  if (!isRecord(locator)) return { ok: false, errors: ["Locator must be a JSON object"] }
  if (locator.version !== 2) errors.push("Locator version must be 2")
  if (locator.app !== "securebackup") errors.push("Locator app must be securebackup")
  if (typeof locator.backup_id !== "string" || !isValidBackupId(locator.backup_id)) errors.push("Locator backup_id must be a UUID")
  if (requestedBackupId !== undefined && locator.backup_id !== requestedBackupId) errors.push("Locator backup_id does not match requested backup ID")

  const manifest = locator.manifest
  if (!isRecord(manifest) || manifest.name !== "manifest.age" || manifest.encryption !== "age") errors.push("Locator manifest metadata is invalid")

  const storage = locator.storage
  if (!isRecord(storage) || storage.layout !== "filesystem-v2") errors.push("Locator storage layout is invalid")

  return errors.length === 0 ? { ok: true, errors, locator: locator as PublicLocatorV2 } : { ok: false, errors }
}
