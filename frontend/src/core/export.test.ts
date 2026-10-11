import assert from "node:assert/strict";
import test from "node:test";
import { exportFilename } from "./export.ts";

test("export filenames retain scope, range, and Unicode from server headers", () => {
  assert.equal(
    exportFilename("attachment; filename=diskord-general_from-2026-10-01.md", "fallback.md"),
    "diskord-general_from-2026-10-01.md",
  );
  assert.equal(
    exportFilename(
      "attachment; filename=ascii.zip; filename*=utf-8''diskord-%E9%A2%91%E9%81%93.zip",
      "fallback.zip",
    ),
    "diskord-频道.zip",
  );
  assert.equal(
    exportFilename('attachment; filename="diskord-general part-01.md"', "fallback.md"),
    "diskord-general part-01.md",
  );
  assert.equal(
    exportFilename("attachment; filename*=UTF-8'zh-CN'%E9%A2%91%E9%81%93.md", "fallback.md"),
    "频道.md",
  );
  assert.equal(
    exportFilename("attachment; filename=ascii.zip; filename*=utf-8''%E9", "fallback.zip"),
    "ascii.zip",
  );
});

test("malformed or unsafe download filenames fall back safely", () => {
  for (const header of [
    null,
    "attachment",
    "attachment; filename*=utf-8''%E9",
    'attachment; filename="../escape.zip"',
    "attachment; filename=bad\\name.zip",
    "attachment; filename*=utf-8''bad%0Aname.zip",
  ]) {
    assert.equal(exportFilename(header, "fallback.zip"), "fallback.zip");
  }
});
