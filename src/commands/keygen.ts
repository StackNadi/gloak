import * as age from "age-encryption"
import { chmod, mkdir, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { homedir } from "node:os"

export type GenerateKeypairOptions = {
  homeDir?: string
}

export type GenerateKeypairResult = {
  keyDir: string
  identityFile: string
  recipientFile: string
  identity: string
  recipient: string
}

export async function generateKeypair(options: GenerateKeypairOptions = {}): Promise<GenerateKeypairResult> {
  const home = options.homeDir ?? homedir()
  const keyDir = join(home, ".securebackup")
  const identityFile = join(keyDir, "identity.txt")
  const recipientFile = join(keyDir, "recipient.txt")

  await mkdir(keyDir, { recursive: true, mode: 0o700 })
  await chmod(keyDir, 0o700)

  const identity = await age.generateIdentity()
  const recipient = await age.identityToRecipient(identity)

  await writeFile(identityFile, `${identity}\n`, { mode: 0o600, flag: "wx" })
  await chmod(identityFile, 0o600)
  await writeFile(recipientFile, `${recipient}\n`, { mode: 0o644, flag: "wx" })
  await chmod(recipientFile, 0o644)

  return { keyDir, identityFile, recipientFile, identity, recipient }
}
