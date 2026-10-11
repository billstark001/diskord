import assert from "node:assert/strict";
import test from "node:test";
import { localTimeBound } from "./time.ts";

test("local export bounds retain seconds and fractional precision", () => {
  process.env.TZ = "Asia/Tokyo";
  assert.equal(localTimeBound(""), "");
  assert.equal(localTimeBound("2026-10-01T12:30"), "2026-10-01T03:30:00.000Z");
  assert.equal(localTimeBound("2026-10-01T12:30:45.12"), "2026-10-01T03:30:45.120Z");
  for (const raw of ["2026-02-30T12:00", "2026-10-01T24:00", "2026-10-01", "bad"])
    assert.throws(() => localTimeBound(raw));
});

test("DST gaps fail instead of silently changing the selected export range", () => {
  process.env.TZ = "America/New_York";
  assert.throws(() => localTimeBound("2026-03-08T02:30"));
  assert.throws(() => localTimeBound("2026-03-08T02:30:00.123"));
  assert.equal(localTimeBound("2026-03-08T01:30"), "2026-03-08T06:30:00.000Z");
  assert.equal(localTimeBound("2026-03-08T03:30"), "2026-03-08T07:30:00.000Z");
  // Fall-back times exist twice; retain the browser's resolution of that time.
  assert.equal(localTimeBound("2026-11-01T01:30"), "2026-11-01T05:30:00.000Z");
});

test("a skipped local date cannot become a different date in the export", () => {
  process.env.TZ = "Pacific/Apia";
  assert.throws(() => localTimeBound("2011-12-30T12:00"));
});
