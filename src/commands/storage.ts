import { LocalStorageBackend } from "../storage/local"
import { parseDestination, type StorageBackend } from "../storage/types"

export function resolveStorage(destination: string): { backend: StorageBackend; backendName: string; root: string } {
  const parsed = parseDestination(destination)
  if (parsed.backend === "local") {
    return { backend: new LocalStorageBackend(parsed.root), backendName: "local", root: parsed.root }
  }
  throw new Error("Unsupported backend")
}
