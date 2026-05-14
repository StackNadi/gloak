#!/usr/bin/env bun
import { uploadBackup } from "./commands/upload"
import { restoreBackup } from "./commands/restore"
import { verifyBackup } from "./commands/verify"
import { listBackups } from "./commands/list"
import { encryptDirect } from "./commands/encrypt"
import { decryptDirect } from "./commands/decrypt"
import { generateKeypair } from "./commands/keygen"
import { resolveRecipient, resolveIdentity } from "./core/resolver"
import { parseSize } from "./core/paths"

function usage(): string {
  return `securebackup — Bun CLI for age-encrypted chunked backups

Usage:
  securebackup keygen                    [-o, --output <dir>]
  securebackup upload <file>             [-r, --recipient <file>] --to <storage> [--chunk-size 20MB]
  securebackup restore <backup-id>       [-i, --identity <file>] --from <storage> --output <path>
  securebackup encrypt <file>            [-r, --recipient <file>] --output <file.age>
  securebackup encrypt <file>            [-r, --recipient <file>] --out-dir <chunks-dir> [--chunk-size 20MB]
  securebackup decrypt <file.age>        [-i, --identity <file>] --output <file>
  securebackup decrypt --chunks-dir <dir> [-i, --identity <file>] --output <file>
  securebackup verify <backup-id>        --from <storage>
  securebackup list                      --from <storage>

-i, --identity <file>     Identity (private key) file  (default: ~/.securebackup/identity.txt)
-r, --recipient <file>    Recipient (public key) file  (default: ~/.securebackup/recipient.txt)
-o, --output <dir>        Output directory for keygen  (default: ~/.securebackup)

Run "securebackup keygen" first to set up keys. After that, -i and -r are
optional — SecureBackup reads from ~/.securebackup automatically.`
}

type Args = { command?: string; positional?: string; flags: Record<string, string> }

function parseArgs(argv: string[]): Args {
  const [command, positional, ...rest] = argv
  const flags: Record<string, string> = {}

  for (let i = 0; i < rest.length; i++) {
    const item = rest[i]

    // --long flags
    if (item.startsWith("--")) {
      const key = item.slice(2)
      const value = rest[i + 1]
      if (value === undefined || value.startsWith("-")) throw new Error(`Missing value for --${key}`)
      flags[key] = value
      i++
      continue
    }

    // Short flags (-i, -r, -o, -h)
    if (item.startsWith("-") && item.length === 2) {
      const key = item.slice(1)
      if (key === "h") {
        console.log(usage())
        process.exit(0)
      }
      const value = rest[i + 1]
      if (value === undefined || value.startsWith("-")) throw new Error(`Missing value for -${key}`)
      flags[key] = value
      i++
      continue
    }

    throw new Error(`Unexpected argument: ${item}`)
  }

  return { command, positional, flags }
}

/** Normalise short flags to their long-form key. */
function normaliseFlags(flags: Record<string, string>): Record<string, string> {
  const f = { ...flags }
  if (f.i && !f.identity) f.identity = f.i
  if (f.r && !f.recipient) f.recipient = f.r
  if (f.o && !f.output) f.output = f.o
  return f
}

async function main(): Promise<void> {
  const { command, positional, flags: raw } = parseArgs(Bun.argv.slice(2))
  const flags = normaliseFlags(raw)

  if (!command || command === "help" || command === "--help") {
    console.log(usage())
    return
  }

  if (command === "keygen") {
    const result = await generateKeypair({ outputDir: flags.output })
    console.log(`Keypair generated.\n\nRecipient:\n${result.recipient}\n\nIdentity file:\n${result.identityFile}\n\nRecipient file:\n${result.recipientFile}`)
    return
  }

  if (command === "upload") {
    if (!positional || !flags.to) throw new Error(`upload requires <file> and --to\n\n${usage()}`)
    const recipient = await resolveRecipient(flags.recipient)
    const result = await uploadBackup({
      inputFile: positional,
      recipient,
      to: flags.to,
      chunkSize: flags["chunk-size"] ? parseSize(flags["chunk-size"]) : undefined,
    })
    console.log(`Backup uploaded successfully.\n\nBackup ID:\n${result.backupId}\n\nChunks:\n${result.chunks}\n\nRemote:\n${result.remote}`)
    return
  }

  if (command === "restore") {
    if (!positional || !flags.from || !flags.output) throw new Error(`restore requires <backup_id>, --from, and --output\n\n${usage()}`)
    const identity = await resolveIdentity(flags.identity)
    const result = await restoreBackup({ backupId: positional, from: flags.from, identity, output: flags.output })
    console.log(`Backup restored successfully.\n\nOutput:\n${result.outputFile}`)
    return
  }

  if (command === "encrypt") {
    if (!positional || (!flags.output && !flags["out-dir"])) {
      throw new Error(`encrypt requires <file> and either --output or --out-dir\n\n${usage()}`)
    }
    const recipient = await resolveRecipient(flags.recipient)
    const result = await encryptDirect({
      inputFile: positional,
      recipient,
      outputFile: flags.output,
      chunksDir: flags["out-dir"],
      chunkSize: flags["chunk-size"] ? parseSize(flags["chunk-size"]) : undefined,
    })
    if (result.chunksDir) {
      console.log(`File encrypted and split successfully.\n\nChunks directory:\n${result.chunksDir}\n\nChunks:\n${result.chunks}`)
    } else {
      console.log(`File encrypted successfully.\n\nOutput:\n${result.outputFile}\n\nBytes:\n${result.bytes}`)
    }
    return
  }

  if (command === "decrypt") {
    if ((!positional && !flags["chunks-dir"]) || !flags.output) {
      throw new Error(`decrypt requires <file.age> or --chunks-dir, plus --output\n\n${usage()}`)
    }
    const identity = await resolveIdentity(flags.identity)
    const result = await decryptDirect({
      inputFile: positional,
      chunksDir: flags["chunks-dir"],
      identity,
      outputFile: flags.output,
    })
    console.log(`File decrypted successfully.\n\nOutput:\n${result.outputFile}\n\nBytes:\n${result.bytes}`)
    return
  }

  if (command === "verify") {
    if (!positional || !flags.from) throw new Error(`verify requires <backup_id> and --from\n\n${usage()}`)
    const result = await verifyBackup({ backupId: positional, from: flags.from })
    if (!result.ok) {
      console.error(`Backup verification failed:\n${result.errors.map((e) => `- ${e}`).join("\n")}`)
      process.exit(1)
    }
    console.log("Backup verification passed.")
    return
  }

  if (command === "list") {
    if (!flags.from) throw new Error(`list requires --from\n\n${usage()}`)
    const backups = await listBackups({ from: flags.from })
    console.log(backups.join("\n"))
    return
  }

  throw new Error(`Unknown command: ${command}\n\n${usage()}`)
}

main().catch((error) => {
  console.error(`securebackup: ${(error as Error).message}`)
  process.exit(1)
})
