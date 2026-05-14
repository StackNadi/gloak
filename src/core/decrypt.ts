import * as age from "age-encryption"
import { readFile, writeFile } from "node:fs/promises"

export async function decryptFile(inputFile: string, outputFile: string, identity: string): Promise<void> {
  const ciphertext = await readFile(inputFile)
  const decrypter = new age.Decrypter()
  decrypter.addIdentity(identity)
  const plaintext = await decrypter.decrypt(ciphertext, "uint8array")
  await writeFile(outputFile, plaintext, { mode: 0o600 })
}
