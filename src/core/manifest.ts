import type { ChunkMetadata } from "./chunk"

export type Manifest = {
  version: 1
  backup_id: string
  created_at: string
  app: { name: "securebackup"; version: string; runtime: "bun" }
  source: { original_filename: string; original_size: number }
  encryption: { format: "age"; library: "age-encryption"; mode: "recipient"; recipient: string }
  chunking: { chunk_size: number; total_chunks: number }
  integrity: { encrypted_file_sha256: string }
  storage: { backend: string; layout: "filesystem-v1" }
  chunks: ChunkMetadata[]
}
