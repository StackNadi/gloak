export type Destination =
  | { backend: "local"; root: string }
  | { backend: "rclone"; remote: string; basePath: string }

export type StorageBackend = {
  uploadFile(localPath: string, remotePath: string): Promise<void>
  downloadFile(remotePath: string, localPath: string): Promise<void>
  exists(remotePath: string): Promise<boolean>
  list(prefix: string): Promise<string[]>

  /**
   * Upload an entire directory recursively. Falls back to per-file uploads
   * in backends that don't support batch operations. Rclone backend uses
   * a single `rclone copy` for efficiency.
   */
  uploadDirectory?(localDir: string, remoteDir: string): Promise<void>

  /**
   * Download an entire remote directory recursively.
   */
  downloadDirectory?(remoteDir: string, localDir: string): Promise<void>

  /**
   * Remove a file or directory recursively from storage.
   */
  remove?(remotePath: string): Promise<void>
}

export function parseDestination(value: string): Destination {
  const separator = value.indexOf(":")
  if (separator === -1) throw new Error(`Invalid destination URI: ${value}`)
  const backend = value.slice(0, separator)
  const rest = value.slice(separator + 1)

  if (backend === "local") {
    if (!rest) throw new Error(`Invalid local destination: ${value}`)
    return { backend: "local", root: rest }
  }

  if (backend === "rclone") {
    if (!rest) throw new Error(`Invalid rclone destination: ${value}`)
    const slash = rest.indexOf("/")
    if (slash === -1) throw new Error(`Invalid rclone destination, expected remote:/path: ${value}`)
    const remote = rest.slice(0, slash).replace(/:$/, "")
    const basePath = rest.slice(slash + 1).replace(/\/$/, "")
    if (!remote) throw new Error(`Invalid rclone destination, missing remote name: ${value}`)
    return { backend: "rclone", remote, basePath }
  }

  throw new Error(`Unsupported backend: ${backend}`)
}
