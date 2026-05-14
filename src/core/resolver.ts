import { readFile } from "node:fs/promises"
import { join } from "node:path"
import { homedir } from "node:os"

export function securebackupDir(homeDir?: string): string {
  return join(homeDir ?? homedir(), ".securebackup")
}

export function recipientFilePath(homeDir?: string): string {
  return join(securebackupDir(homeDir), "recipient.txt")
}

export function identityFilePath(homeDir?: string): string {
  return join(securebackupDir(homeDir), "identity.txt")
}

export async function resolveRecipient(flagValue?: string, homeDir?: string): Promise<string> {
  if (flagValue) return flagValue
  const file = recipientFilePath(homeDir)
  try {
    const text = await readFile(file, "utf8")
    const line = text.split("\n").find((l) => l.startsWith("age1"))
    if (!line) throw new Error("empty")
    return line.trim()
  } catch {
    throw new Error(
      `No --recipient given and no recipient file found at ${file}. ` +
      `Run "securebackup keygen" first, or pass --recipient explicitly.`,
    )
  }
}

export async function resolveIdentity(flagValue?: string, homeDir?: string): Promise<string> {
  if (flagValue) return flagValue
  const file = identityFilePath(homeDir)
  try {
    const text = await readFile(file, "utf8")
    const line = text.split("\n").find((l) => l.startsWith("AGE-SECRET-KEY-"))
    if (!line) throw new Error("empty")
    return line.trim()
  } catch {
    throw new Error(
      `No --identity given and no identity file found at ${file}. ` +
      `Run "securebackup keygen" first, or pass --identity explicitly.`,
    )
  }
}
