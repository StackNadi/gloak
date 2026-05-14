import { readFile } from "node:fs/promises"
import { join, isAbsolute } from "node:path"
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

function expandHome(p: string): string {
  return p.startsWith("~") ? join(homedir(), p.slice(1)) : p
}

/**
 * Read a key file (possibly with comments) and return the first line
 * matching `prefix`. Returns empty string if no match.
 */
export async function readKeyFromFile(filePath: string, prefix: string): Promise<string> {
  const text = await readFile(expandHome(filePath), "utf8")
  const line = text.split("\n").find((l) => l.startsWith(prefix))
  return line?.trim() ?? ""
}

/**
 * Resolve recipient key.
 *
 * If `filePath` is given, it's treated as a **file path** (like SSH's -i):
 * the key is read from that file. Otherwise falls back to
 * ~/.securebackup/recipient.txt.
 */
export async function resolveRecipient(filePath?: string, homeDir?: string): Promise<string> {
  if (filePath) {
    try {
      const key = await readKeyFromFile(filePath, "age1")
      if (!key) throw new Error("empty")
      return key
    } catch (err) {
      const detail = (err as NodeJS.ErrnoException).code === "ENOENT"
        ? `No such file: ${filePath}`
        : `No age1... key found in file: ${filePath}`
      throw new Error(detail)
    }
  }
  const file = recipientFilePath(homeDir)
  try {
    const key = await readKeyFromFile(file, "age1")
    if (key) return key
  } catch {
    // fall through to the error below
  }
  throw new Error(
    `No --recipient given and no recipient file found at ${file}. ` +
    `Run "securebackup keygen" first, or pass -r /path/to/recipient.txt explicitly.`,
  )
}

/**
 * Resolve identity key (private key for decrypting).
 *
 * If `filePath` is given, it's treated as a **file path** (like SSH's -i):
 * the key is read from that file. Otherwise falls back to
 * ~/.securebackup/identity.txt.
 */
export async function resolveIdentity(filePath?: string, homeDir?: string): Promise<string> {
  if (filePath) {
    try {
      const key = await readKeyFromFile(filePath, "AGE-SECRET-KEY-")
      if (!key) throw new Error("empty")
      return key
    } catch (err) {
      const detail = (err as NodeJS.ErrnoException).code === "ENOENT"
        ? `No such file: ${filePath}`
        : `No AGE-SECRET-KEY-... found in file: ${filePath}`
      throw new Error(detail)
    }
  }
  const file = identityFilePath(homeDir)
  try {
    const key = await readKeyFromFile(file, "AGE-SECRET-KEY-")
    if (key) return key
  } catch {
    // fall through to the error below
  }
  throw new Error(
    `No --identity given and no identity file found at ${file}. ` +
    `Run "securebackup keygen" first, or pass -i /path/to/identity.txt explicitly.`,
  )
}
