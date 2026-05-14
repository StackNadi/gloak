import { resolveStorage } from "./storage"

export async function listBackups(options: { from: string }): Promise<string[]> {
  const { backend } = resolveStorage(options.from)
  const entries = await backend.list("")
  return entries.map((entry) => entry.replace(/\/$/, "")).filter(Boolean)
}
