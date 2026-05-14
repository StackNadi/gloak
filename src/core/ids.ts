import { randomUUID } from "node:crypto"

export function newBackupId(): string {
  return randomUUID()
}
