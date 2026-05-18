import { describe, expect, test } from "bun:test"
import { parseDestination } from "../src/storage/types"

describe("destination URI parsing", () => {
  test("parses local destinations", () => {
    expect(parseDestination("local:/mnt/backups")).toEqual({ backend: "local", root: "/mnt/backups" })
  })

  test("parses rclone destinations", () => {
    expect(parseDestination("rclone:keraaaki:/backups")).toEqual({ backend: "rclone", remote: "keraaaki", basePath: "backups" })
  })

  test("parses rclone destinations at root", () => {
    expect(parseDestination("rclone:keraaaki:/")).toEqual({ backend: "rclone", remote: "keraaaki", basePath: "" })
  })

  test("parses rclone destinations with nested path", () => {
    expect(parseDestination("rclone:gdrive:/videos/private")).toEqual({ backend: "rclone", remote: "gdrive", basePath: "videos/private" })
  })

  test("parses rclone destinations with trailing slash", () => {
    expect(parseDestination("rclone:keraaaki:/backups/")).toEqual({ backend: "rclone", remote: "keraaaki", basePath: "backups" })
  })

  test("rejects rclone destinations without slash", () => {
    expect(() => parseDestination("rclone:keraaaki")).toThrow(/Invalid rclone destination/)
  })

  test("rejects rclone destinations without remote name", () => {
    expect(() => parseDestination("rclone:/backups")).toThrow(/Invalid rclone destination/)
  })

  test("rejects unsupported backends", () => {
    expect(() => parseDestination("ftp:/nope")).toThrow(/Unsupported backend/)
  })

  test("rejects malformed local destinations", () => {
    expect(() => parseDestination("local:")).toThrow(/Invalid local destination/)
  })

  test("rejects destinations without a colon", () => {
    expect(() => parseDestination("justapath")).toThrow(/Invalid destination/)
  })
})
