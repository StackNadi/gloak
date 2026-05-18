import { randomUUID } from "node:crypto"

const BACKUP_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

export function newBackupId(): string {
  return randomUUID()
}

export function isValidBackupId(value: string): boolean {
  return BACKUP_ID.test(value)
}

export function assertValidBackupId(value: string): void {
  if (!isValidBackupId(value)) throw new Error(`Invalid backup ID: ${value}`)
}
