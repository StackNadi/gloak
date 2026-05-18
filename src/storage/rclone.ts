import { mkdir } from "node:fs/promises"
import { existsSync } from "node:fs"
import { join } from "node:path"
import { homedir } from "node:os"
import { execSync } from "node:child_process"
import type { StorageBackend } from "./types"

const BASE_OPTS = {
  encoding: "utf8" as const,
  stdio: ["pipe", "pipe", "pipe"] as ["pipe", "pipe", "pipe"],
}

function cleanArg(a: string): string {
  // Just wrap in quotes, escape inner quotes
  return `"${a.replace(/\"/g, '\\"')}"`
}

/** Find the rclone config file. Hermes changes $HOME so the default
 *  ~/.config/rclone/rclone.conf may point to the wrong place. */
function findConfig(): string {
  const candidates = [
    join(homedir(), ".config", "rclone", "rclone.conf"),
    "/home/keraki/.config/rclone/rclone.conf",
    "/root/.config/rclone/rclone.conf",
  ]
  for (const path of candidates) {
    if (existsSync(path)) return path
  }
  return join(homedir(), ".config", "rclone", "rclone.conf")
}

const RCLONE_CONFIG = findConfig()

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function rclone(args: string[], timeoutMs?: number): { stdout: string; stderr: string; exitCode: number } {
  const joined = args.map(cleanArg).join(" ")
  const cmd = `rclone --config "${RCLONE_CONFIG}" ${joined}`
  const opts = timeoutMs !== undefined ? { ...BASE_OPTS, timeout: timeoutMs } : BASE_OPTS
  try {
    const stdout = execSync(cmd, opts)
    return { stdout, stderr: "", exitCode: 0 }
  } catch (error: unknown) {
    const err = error as Error & { stdout?: string; stderr?: string; status?: number; code?: string }
    if (err.code === "ETIMEDOUT" || (err as { killed?: boolean }).killed) {
      return {
        stdout: "",
        stderr: `rclone command timed out after ${timeoutMs ?? 0}ms. Check your connection or try manually: ${cmd}`,
        exitCode: 124,
      }
    }
    return {
      stdout: err.stdout ?? "",
      stderr: err.stderr ?? "",
      exitCode: err.status ?? 1,
    }
  }
}

/**
 * Run an rclone operation with retries.
 * Each attempt has a 5-minute timeout. On failure, waits with exponential
 * backoff (2s, 4s, 6s) before retrying, up to `maxAttempts` total.
 */
async function rcloneWithRetry(
  args: string[],
  label: string,
  maxAttempts = 3,
): Promise<void> {
  let lastErr: Error | null = null
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    const result = rclone(args, 5 * 60_000)
    if (result.exitCode === 0) return
    lastErr = new Error(`${label}: ${result.stderr || result.stdout}`.trim())
    if (attempt < maxAttempts) {
      await sleep(attempt * 2000) // 2s, 4s, 6s
    }
  }
  throw lastErr!
}

function checkRemote(remote: string): void {
  const result = rclone(["listremotes"], 15_000)
  if (result.exitCode !== 0) {
    throw new Error(
      `rclone is not available or config not found.\nConfig searched: ${RCLONE_CONFIG}\nInstall rclone: sudo apt install rclone\nThen run: rclone config`,
    )
  }
  const remotes = result.stdout.trim().split("\n").filter(Boolean).map((r) => r.replace(/:$/, ""))
  if (!remotes.includes(remote)) {
    throw new Error(
      `rclone remote "${remote}" not found. Configure it: rclone config\nAvailable remotes: ${remotes.join(", ") || "(none)"}`,
    )
  }
}

export class RcloneStorageBackend implements StorageBackend {
  private readonly remote: string
  private readonly basePath: string
  private checked = false

  constructor(remote: string, basePath: string) {
    this.remote = remote
    this.basePath = basePath
  }

  private ensureRemote(): void {
    if (!this.checked) {
      checkRemote(this.remote)
      this.checked = true
    }
  }

  private remotePath(path: string): string {
    const prefix = this.basePath ? `${this.basePath}/` : ""
    return `${this.remote}:${prefix}${path}`
  }

  async uploadFile(localPath: string, remotePath: string): Promise<void> {
    this.ensureRemote()
    await rcloneWithRetry(["copyto", localPath, this.remotePath(remotePath)], "rclone upload failed")
  }

  async downloadFile(remotePath: string, localPath: string): Promise<void> {
    this.ensureRemote()
    await mkdir(join(localPath, ".."), { recursive: true })
    await rcloneWithRetry(["copyto", this.remotePath(remotePath), localPath], "rclone download failed")
  }

  async exists(remotePath: string): Promise<boolean> {
    this.ensureRemote()
    const result = rclone(["lsf", this.remotePath(remotePath)], 30_000)
    if (result.exitCode !== 0) return false
    return result.stdout.trim().length > 0
  }

  async list(prefix: string): Promise<string[]> {
    this.ensureRemote()
    const result = rclone(["lsf", this.remotePath(prefix)], 30_000)
    if (result.exitCode !== 0) return []
    return result.stdout.trim().split("\n").filter(Boolean)
  }

  async remove(remotePath: string): Promise<void> {
    this.ensureRemote()
    // purge removes the directory and all its contents recursively
    const result = rclone(["purge", this.remotePath(remotePath)])
    if (result.exitCode !== 0) {
      // If it doesn't exist, that's fine
      if (result.stderr?.includes("directory not found") || result.stderr?.includes("doesn't exist")) return
      throw new Error(`rclone remove failed: ${result.stderr || result.stdout}`)
    }
  }
}
