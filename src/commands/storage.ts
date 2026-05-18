import { LocalStorageBackend } from "../storage/local"
import { RcloneStorageBackend } from "../storage/rclone"
import { parseDestination, type StorageBackend } from "../storage/types"

export function resolveStorage(destination: string): { backend: StorageBackend; backendName: string; root: string } {
  const parsed = parseDestination(destination)
  if (parsed.backend === "local") {
    return { backend: new LocalStorageBackend(parsed.root), backendName: "local", root: parsed.root }
  }
  if (parsed.backend === "rclone") {
    return {
      backend: new RcloneStorageBackend(parsed.remote, parsed.basePath),
      backendName: "rclone",
      root: `${parsed.remote}:${parsed.basePath}`,
    }
  }
  throw new Error("Unsupported backend")
}
