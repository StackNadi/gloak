import * as age from "age-encryption"
import { readFile, writeFile } from "node:fs/promises"

export async function encryptFile(inputFile: string, outputFile: string, recipient: string): Promise<void> {
  const plaintext = await readFile(inputFile)
  const encrypter = new age.Encrypter()
  encrypter.addRecipient(recipient)
  const ciphertext = await encrypter.encrypt(plaintext)
  await writeFile(outputFile, ciphertext, { mode: 0o600 })
}
