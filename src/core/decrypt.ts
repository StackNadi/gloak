import { createReadStream } from "node:fs"
import { open } from "node:fs/promises"
import { Readable } from "node:stream"
import { pipeline } from "node:stream/promises"
import * as age from "age-encryption"

export async function decryptFile(inputFile: string, outputFile: string, identity: string): Promise<void> {
  const decrypter = new age.Decrypter()
  decrypter.addIdentity(identity)

  const nodeInput = createReadStream(inputFile)
  const webInput = Readable.toWeb(nodeInput) as ReadableStream<Uint8Array>
  const webOutput = await decrypter.decrypt(webInput)

  // Write the decrypted stream to the output file with restricted permissions
  const fileHandle = await open(outputFile, "w", 0o600)
  const nodeOutput = Readable.fromWeb(webOutput as ReadableStream<Uint8Array>)
  const writeStream = fileHandle.createWriteStream()

  try {
    await pipeline(nodeOutput, writeStream)
  } finally {
    try {
      writeStream.close()
    } catch {
      // ignore close errors
    }
    try {
      await fileHandle.close()
    } catch {
      // ignore close errors
    }
  }
}
