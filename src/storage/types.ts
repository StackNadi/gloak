export type Destination =
  | { backend: "local"; root: string }

export type StorageBackend = {
  uploadFile(localPath: string, remotePath: string): Promise<void>
  downloadFile(remotePath: string, localPath: string): Promise<void>
  exists(remotePath: string): Promise<boolean>
  list(prefix: string): Promise<string[]>
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

  throw new Error(`Unsupported backend: ${backend}`)
}
