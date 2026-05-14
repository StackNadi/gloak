import { stat } from "node:fs/promises"
import { encryptFile } from "../core/encrypt"

export type EncryptDirectOptions = {
  inputFile: string
  outputFile: string
  recipient: string
}

export async function encryptDirect(options: EncryptDirectOptions): Promise<{ outputFile: string; bytes: number }> {
  await encryptFile(options.inputFile, options.outputFile, options.recipient)
  const info = await stat(options.outputFile)
  return { outputFile: options.outputFile, bytes: info.size }
}
