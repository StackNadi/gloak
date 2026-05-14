import { stat } from "node:fs/promises"
import { decryptFile } from "../core/decrypt"

export type DecryptDirectOptions = {
  inputFile: string
  outputFile: string
  identity: string
}

export async function decryptDirect(options: DecryptDirectOptions): Promise<{ outputFile: string; bytes: number }> {
  await decryptFile(options.inputFile, options.outputFile, options.identity)
  const info = await stat(options.outputFile)
  return { outputFile: options.outputFile, bytes: info.size }
}
