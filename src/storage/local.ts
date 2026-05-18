import { chmod, copyFile, mkdir, readdir, rm, stat as asyncStat } from "node:fs/promises"
import { existsSync } from "node:fs"
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path"
import type { StorageBackend } from "./types"

function hasUnsafeSegment(path: string): boolean {
  return path.split(/[\\/]+/).some((segment) => segment === ".." || segment === "")
}

/** Call chmod but ignore errors from filesystems that don't support permissions. */
async function tryChmod(path: string, mode: number): Promise<void> {
  try {
    await chmod(path, mode)
  } catch (error: unknown) {
    const err = error as NodeJS.ErrnoException
    // FUSE mounts, rclone, network drives, Windows — none support chmod.
    if (err.code === "ENOTSUP" || err.code === "EPERM" || err.code === "ENOENT" || err.code === "EINVAL" || err.code === "EROFS" || err.code === "EACCES") {
      return
    }
    throw error
  }
}

/** Call mkdir but ignore chmod errors on the created directory. */
async function tryMkdir(dir: string): Promise<void> {
  try {
    await mkdir(dir, { recursive: true, mode: 0o700 })
  } catch {
    // existsSync check happens before write, so mkdir should succeed
    // or the dir already exists. Either way it's fine.
  }
}

function exists(path: string): boolean {
  return existsSync(path)
}

export class LocalStorageBackend implements StorageBackend {
  private readonly rootPath: string

  constructor(root: string) {
    this.rootPath = resolve(root)
  }

  private resolve(remotePath: string): string {
    if (isAbsolute(remotePath) || hasUnsafeSegment(remotePath)) {
      throw new Error(`Unsafe remote path: ${remotePath}`)
    }

    const target = resolve(this.rootPath, remotePath)
    const relation = relative(this.rootPath, target)
    if (relation === ".." || relation.startsWith(`..${sep}`) || isAbsolute(relation)) {
      throw new Error(`Remote path escapes outside storage root: ${remotePath}`)
    }
    return target
  }

  async uploadFile(localPath: string, remotePath: string): Promise<void> {
    const target = this.resolve(remotePath)
    await tryMkdir(dirname(target))
    await tryChmod(dirname(target), 0o700)
    if (exists(target)) throw new Error(`Remote file already exists: ${remotePath}`)
    await copyFile(localPath, target)
    await tryChmod(target, 0o600)
  }

  async downloadFile(remotePath: string, localPath: string): Promise<void> {
    await tryMkdir(dirname(localPath))
    await copyFile(this.resolve(remotePath), localPath)
    await tryChmod(localPath, 0o600)
  }

  async exists(remotePath: string): Promise<boolean> {
    return exists(this.resolve(remotePath))
  }

  async list(prefix: string): Promise<string[]> {
    const dir = prefix === "" ? this.rootPath : this.resolve(prefix)
    if (!exists(dir)) return []
    const entries = await readdir(dir)
    const paths: string[] = []
    for (const entry of entries) {
      const full = join(dir, entry)
      const info = await asyncStat(full)
      if (info.isDirectory()) paths.push(join(prefix, entry))
      else paths.push(join(prefix, entry))
    }
    return paths
  }

  async uploadDirectory(localDir: string, remoteDir: string): Promise<void> {
    const target = this.resolve(remoteDir)
    await tryMkdir(target)

    const names = await readdir(localDir)
    for (const name of names) {
      const src = join(localDir, name)
      const dst = join(target, name)
      const info = await asyncStat(src)
      if (info.isDirectory()) {
        await this.uploadDirectory(src, `${remoteDir}/${name}`)
      } else {
        await copyFile(src, dst)
        await tryChmod(dst, 0o600)
      }
    }
  }

  async downloadDirectory(remoteDir: string, localDir: string): Promise<void> {
    await tryMkdir(localDir)

    const names = await readdir(this.resolve(remoteDir))
    for (const name of names) {
      const src = this.resolve(`${remoteDir}/${name}`)
      const dst = join(localDir, name)
      const info = await asyncStat(src)
      if (info.isDirectory()) {
        await this.downloadDirectory(`${remoteDir}/${name}`, dst)
      } else {
        await copyFile(src, dst)
        await tryChmod(dst, 0o600)
      }
    }
  }

  async remove(remotePath: string): Promise<void> {
    await rm(this.resolve(remotePath), { recursive: true, force: true })
  }
}
