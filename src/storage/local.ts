import { copyFile, mkdir, readdir, stat } from "node:fs/promises"
import { existsSync } from "node:fs"
import { dirname, join } from "node:path"
import type { StorageBackend } from "./types"

export class LocalStorageBackend implements StorageBackend {
  constructor(private root: string) {}

  private resolve(remotePath: string): string {
    return join(this.root, remotePath)
  }

  async uploadFile(localPath: string, remotePath: string): Promise<void> {
    const target = this.resolve(remotePath)
    await mkdir(dirname(target), { recursive: true })
    await copyFile(localPath, target)
  }

  async downloadFile(remotePath: string, localPath: string): Promise<void> {
    await mkdir(dirname(localPath), { recursive: true })
    await copyFile(this.resolve(remotePath), localPath)
  }

  async exists(remotePath: string): Promise<boolean> {
    return existsSync(this.resolve(remotePath))
  }

  async list(prefix: string): Promise<string[]> {
    const dir = this.resolve(prefix)
    if (!existsSync(dir)) return []
    const entries = await readdir(dir)
    const paths: string[] = []
    for (const entry of entries) {
      const full = join(dir, entry)
      const info = await stat(full)
      if (info.isDirectory()) paths.push(join(prefix, entry))
      else paths.push(join(prefix, entry))
    }
    return paths
  }
}
