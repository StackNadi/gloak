import { describe, expect, test } from "bun:test"
import { parseDestination } from "../src/storage/types"

describe("destination URI parsing", () => {
  test("parses local destinations", () => {
    expect(parseDestination("local:/mnt/backups")).toEqual({ backend: "local", root: "/mnt/backups" })
  })

  test("rejects unsupported backends", () => {
    expect(() => parseDestination("ftp:/nope")).toThrow(/Unsupported backend/)
  })

  test("rejects malformed destinations", () => {
    expect(() => parseDestination("local:")).toThrow(/Invalid local destination/)
  })
})
