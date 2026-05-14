#!/usr/bin/env bun
import { uploadBackup } from "./commands/upload"
import { restoreBackup } from "./commands/restore"
import { verifyBackup } from "./commands/verify"
import { listBackups } from "./commands/list"
import { encryptDirect } from "./commands/encrypt"
import { decryptDirect } from "./commands/decrypt"
import { parseSize } from "./core/paths"

function usage(): string {
  return `securebackup — Bun CLI for age-encrypted chunked backups

Usage:
  securebackup upload <file> --recipient <age1...> --to local:/path [--chunk-size 20MB]
  securebackup restore <backup_id> --from local:/path --identity <AGE-SECRET-KEY...> --output <file-or-dir>
  securebackup encrypt <file> --recipient <age1...> --output <file.age>
  securebackup encrypt <file> --recipient <age1...> --out-dir <chunks-dir> [--chunk-size 20MB]
  securebackup decrypt <file.age> --identity <AGE-SECRET-KEY...> --output <file>
  securebackup verify <backup_id> --from local:/path
  securebackup list --from local:/path
`
}

function parseArgs(argv: string[]): { command?: string; positional?: string; flags: Record<string, string> } {
  const [command, positional, ...rest] = argv
  const flags: Record<string, string> = {}
  for (let i = 0; i < rest.length; i++) {
    const item = rest[i]
    if (!item.startsWith("--")) throw new Error(`Unexpected argument: ${item}`)
    const value = rest[i + 1]
    if (!value || value.startsWith("--")) throw new Error(`Missing value for ${item}`)
    flags[item.slice(2)] = value
    i++
  }
  return { command, positional, flags }
}

async function main(): Promise<void> {
  const { command, positional, flags } = parseArgs(Bun.argv.slice(2))
  if (!command || command === "help" || command === "--help" || command === "-h") {
    console.log(usage())
    return
  }

  if (command === "upload") {
    if (!positional || !flags.recipient || !flags.to) throw new Error(`upload requires <file>, --recipient, and --to\n\n${usage()}`)
    const result = await uploadBackup({
      inputFile: positional,
      recipient: flags.recipient,
      to: flags.to,
      chunkSize: flags["chunk-size"] ? parseSize(flags["chunk-size"]) : undefined,
    })
    console.log(`Backup uploaded successfully.\n\nBackup ID:\n${result.backupId}\n\nChunks:\n${result.chunks}\n\nRemote:\n${result.remote}`)
    return
  }

  if (command === "restore") {
    if (!positional || !flags.from || !flags.identity || !flags.output) throw new Error(`restore requires <backup_id>, --from, --identity, and --output\n\n${usage()}`)
    const result = await restoreBackup({ backupId: positional, from: flags.from, identity: flags.identity, output: flags.output })
    console.log(`Backup restored successfully.\n\nOutput:\n${result.outputFile}`)
    return
  }

  if (command === "encrypt") {
    if (!positional || !flags.recipient || (!flags.output && !flags["out-dir"])) {
      throw new Error(`encrypt requires <file>, --recipient, and either --output or --out-dir\n\n${usage()}`)
    }
    const result = await encryptDirect({
      inputFile: positional,
      recipient: flags.recipient,
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
    if (!positional || !flags.identity || !flags.output) throw new Error(`decrypt requires <file.age>, --identity, and --output\n\n${usage()}`)
    const result = await decryptDirect({ inputFile: positional, identity: flags.identity, outputFile: flags.output })
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
