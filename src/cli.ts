#!/usr/bin/env bun
import { defineCommand, runCommand, showUsage } from "citty"
import { consola } from "consola"
import { uploadBackup } from "./commands/upload"
import { restoreBackup } from "./commands/restore"
import { verifyBackup } from "./commands/verify"
import { listBackups } from "./commands/list"
import { encryptDirect } from "./commands/encrypt"
import { decryptDirect } from "./commands/decrypt"
import { generateKeypair } from "./commands/keygen"
import { resolveRecipient, resolveIdentity } from "./core/resolver"
import { parseSize } from "./core/paths"
import { listCheckpoints } from "./core/checkpoint"

type CliArgs = Record<string, unknown>
const logger = consola.create({ level: 3 })

function optionalString(args: CliArgs, key: string): string | undefined {
  const value = args[key]
  return typeof value === "string" && value.length > 0 ? value : undefined
}

function requiredString(args: CliArgs, key: string, label = key): string {
  const value = optionalString(args, key)
  if (!value) throw new Error(`Missing required argument: ${label}`)
  return value
}

const keygenCommand = defineCommand({
  meta: {
    name: "keygen",
    description: "Generate an age identity and recipient file",
  },
  args: {
    output: {
      type: "string",
      alias: "o",
      valueHint: "dir",
      description: "Output directory for identity.txt and recipient.txt",
    },
  },
  async run({ args }) {
    const result = await generateKeypair({ outputDir: optionalString(args, "output") })
    logger.log(
      `Keypair generated.\n\nRecipient:\n${result.recipient}\n\nIdentity file:\n${result.identityFile}\n\nRecipient file:\n${result.recipientFile}`,
    )
  },
})

const uploadCommand = defineCommand({
  meta: {
    name: "upload",
    description: "Encrypt, chunk, and upload a file to a storage destination",
  },
  args: {
    file: {
      type: "positional",
      required: true,
      valueHint: "file",
      description: "File to back up",
    },
    recipient: {
      type: "string",
      alias: "r",
      valueHint: "file",
      description: "Recipient public key file (defaults to ~/.securebackup/recipient.txt)",
    },
    to: {
      type: "string",
      required: true,
      valueHint: "storage",
      description: "Destination URI, for example local:/mnt/backups",
    },
    "chunk-size": {
      type: "string",
      valueHint: "size",
      description: "Chunk size, for example 20MB",
      default: "20MB",
    },
    resume: {
      type: "string",
      valueHint: "backup-id",
      description:
        "Resume a failed upload. Provide the backup ID from a previous partial upload. Skips re-encryption if the workdir still exists.",
    },
  },
  async run({ args }) {
    const recipient = await resolveRecipient(optionalString(args, "recipient"))
    const resumeBackupId = optionalString(args, "resume")
    const result = await uploadBackup({
      inputFile: requiredString(args, "file", "<file>"),
      recipient,
      to: requiredString(args, "to", "--to"),
      chunkSize: parseSize(requiredString(args, "chunkSize", "--chunk-size")),
      resumeBackupId,
    })
    logger.log(`Backup uploaded successfully.\n\nBackup ID:\n${result.backupId}\n\nChunks:\n${result.chunks}\n\nRemote:\n${result.remote}`)
  },
})

const restoreCommand = defineCommand({
  meta: {
    name: "restore",
    description: "Restore and decrypt a backup from storage",
  },
  args: {
    backupId: {
      type: "positional",
      required: true,
      valueHint: "backup-id",
      description: "Backup UUID to restore",
    },
    from: {
      type: "string",
      required: true,
      valueHint: "storage",
      description: "Source URI, for example local:/mnt/backups",
    },
    identity: {
      type: "string",
      alias: "i",
      valueHint: "file",
      description: "Identity private key file (defaults to ~/.securebackup/identity.txt)",
    },
    output: {
      type: "string",
      required: true,
      valueHint: "path",
      description: "Output file or directory",
    },
  },
  async run({ args }) {
    const identity = await resolveIdentity(optionalString(args, "identity"))
    const result = await restoreBackup({
      backupId: requiredString(args, "backupId", "<backup-id>"),
      from: requiredString(args, "from", "--from"),
      identity,
      output: requiredString(args, "output", "--output"),
    })
    logger.log(`Backup restored successfully.\n\nOutput:\n${result.outputFile}`)
  },
})

const encryptCommand = defineCommand({
  meta: {
    name: "encrypt",
    description: "Encrypt a file directly, optionally splitting the encrypted output into chunks",
  },
  args: {
    file: {
      type: "positional",
      required: true,
      valueHint: "file",
      description: "File to encrypt",
    },
    recipient: {
      type: "string",
      alias: "r",
      valueHint: "file",
      description: "Recipient public key file (defaults to ~/.securebackup/recipient.txt)",
    },
    output: {
      type: "string",
      valueHint: "file.age",
      description: "Encrypted output file",
    },
    "out-dir": {
      type: "string",
      valueHint: "chunks-dir",
      description: "Directory to write encrypted chunks instead of one file",
    },
    "chunk-size": {
      type: "string",
      valueHint: "size",
      description: "Chunk size when --out-dir is used",
      default: "20MB",
    },
  },
  async run({ args }) {
    const outputFile = optionalString(args, "output")
    const chunksDir = optionalString(args, "outDir")
    if (!outputFile && !chunksDir) {
      throw new Error("encrypt requires either --output or --out-dir")
    }

    const recipient = await resolveRecipient(optionalString(args, "recipient"))
    const result = await encryptDirect({
      inputFile: requiredString(args, "file", "<file>"),
      recipient,
      outputFile,
      chunksDir,
      chunkSize: parseSize(requiredString(args, "chunkSize", "--chunk-size")),
    })
    if (result.chunksDir) {
      logger.log(`File encrypted and split successfully.\n\nChunks directory:\n${result.chunksDir}\n\nChunks:\n${result.chunks}`)
    } else {
      logger.log(`File encrypted successfully.\n\nOutput:\n${result.outputFile}\n\nBytes:\n${result.bytes}`)
    }
  },
})

const decryptCommand = defineCommand({
  meta: {
    name: "decrypt",
    description: "Decrypt a direct encrypted file or chunk directory",
  },
  args: {
    file: {
      type: "positional",
      required: false,
      valueHint: "file.age",
      description: "Encrypted file to decrypt",
    },
    "chunks-dir": {
      type: "string",
      valueHint: "dir",
      description: "Directory containing encrypted chunks",
    },
    manifest: {
      type: "string",
      valueHint: "manifest.json",
      description: "Manifest file used to verify chunk integrity when --chunks-dir is used",
    },
    identity: {
      type: "string",
      alias: "i",
      valueHint: "file",
      description: "Identity private key file (defaults to ~/.securebackup/identity.txt)",
    },
    output: {
      type: "string",
      required: true,
      valueHint: "file",
      description: "Output plaintext file",
    },
  },
  async run({ args }) {
    const inputFile = optionalString(args, "file")
    const chunksDir = optionalString(args, "chunksDir")
    if (!inputFile && !chunksDir) {
      throw new Error("decrypt requires <file.age> or --chunks-dir")
    }

    const identity = await resolveIdentity(optionalString(args, "identity"))
    const result = await decryptDirect({
      inputFile,
      chunksDir,
      manifestFile: optionalString(args, "manifest"),
      identity,
      outputFile: requiredString(args, "output", "--output"),
    })
    logger.log(`File decrypted successfully.\n\nOutput:\n${result.outputFile}\n\nBytes:\n${result.bytes}`)
  },
})

const verifyCommand = defineCommand({
  meta: {
    name: "verify",
    description: "Verify backup chunks and checksums",
  },
  args: {
    backupId: {
      type: "positional",
      required: true,
      valueHint: "backup-id",
      description: "Backup UUID to verify",
    },
    from: {
      type: "string",
      required: true,
      valueHint: "storage",
      description: "Source URI, for example local:/mnt/backups",
    },
    identity: {
      type: "string",
      alias: "i",
      valueHint: "file",
      description: "Identity private key file (defaults to ~/.securebackup/identity.txt)",
    },
  },
  async run({ args }) {
    const identity = await resolveIdentity(optionalString(args, "identity"))
    const result = await verifyBackup({
      backupId: requiredString(args, "backupId", "<backup-id>"),
      from: requiredString(args, "from", "--from"),
      identity,
    })
    if (!result.ok) {
      logger.error(`Backup verification failed:\n${result.errors.map((error) => `- ${error}`).join("\n")}`)
      process.exit(1)
    }
    logger.log("Backup verification passed.")
  },
})

const listCommand = defineCommand({
  meta: {
    name: "list",
    description: "List backups in a storage destination",
  },
  args: {
    from: {
      type: "string",
      required: true,
      valueHint: "storage",
      description: "Source URI, for example local:/mnt/backups",
    },
  },
  async run({ args }) {
    const backups = await listBackups({ from: requiredString(args, "from", "--from") })
    logger.log(backups.join("\n"))
  },
})

const checkpointsCommand = defineCommand({
  meta: {
    name: "checkpoints",
    description: "List failed uploads available for resuming",
  },
  args: {},
  async run() {
    const all = await listCheckpoints()
    if (all.length === 0) {
      logger.log("No checkpoints found. Failed uploads with checkpoint data will appear here.")
      return
    }
    for (const cp of all) {
      const uploaded = cp.uploadedChunkNames.length
      logger.log(
        `  ${cp.backupId}  ${uploaded}/${cp.totalChunks} chunks  ${cp.updatedAt.slice(0, 19).replace("T", " ")}\n` +
        `    -> ${cp.destination}\n` +
        `    Resume: securebackup upload ${cp.sourceFile} -r ${cp.recipient} --to ${cp.destination} --resume ${cp.backupId}`,
      )
    }
  },
})

const subCommands = {
  keygen: keygenCommand,
  upload: uploadCommand,
  restore: restoreCommand,
  encrypt: encryptCommand,
  decrypt: decryptCommand,
  verify: verifyCommand,
  list: listCommand,
  checkpoints: checkpointsCommand,
}

const main = defineCommand({
  meta: {
    name: "securebackup",
    version: "0.1.0",
    description: "age-encrypted backup tool",
  },
  subCommands,
})

function formatCliError(error: unknown): string {
  if (error instanceof Error && error.message.length > 0) return `Error: ${error.message}`
  if (typeof error === "string" && error.length > 0) return `Error: ${error}`
  return "Error: Command failed"
}

async function mainEntrypoint(): Promise<void> {
  const rawArgs = process.argv.slice(2)
  const wantsHelp = rawArgs.includes("--help") || rawArgs.includes("-h")

  if (wantsHelp) {
    const maybeCommand = subCommands[rawArgs[0] as keyof typeof subCommands]
    await showUsage(maybeCommand ?? main, maybeCommand ? main : undefined)
    return
  }

  if (rawArgs.length === 1 && ["--version", "-v"].includes(rawArgs[0])) {
    logger.log("0.1.0")
    return
  }

  await runCommand(main, { rawArgs })
}

mainEntrypoint().catch((error) => {
  logger.error(formatCliError(error))
  process.exitCode = 1
})
