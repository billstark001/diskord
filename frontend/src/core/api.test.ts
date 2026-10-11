import assert from "node:assert/strict";
import test from "node:test";
import { api, request } from "./api.ts";

test("downloads and JSON requests share server errors and HTTP fallbacks", async (t) => {
  const fetch = t.mock.method(globalThis, "fetch");
  for (const body of ['{"error":"another export is in progress"}', "not JSON", "null"]) {
    fetch.mock.mockImplementation(async () => new Response(body, { status: 429 }));
    const message = body.startsWith("{") ? "another export is in progress" : "HTTP 429";
    await assert.rejects(request("/api/export"), { message });
    await assert.rejects(api("/api/session"), { message });
  }
});

test("requests preserve Headers, cancellation, and binary response bodies", async (t) => {
  const controller = new AbortController();
  const headers = new Headers({ "X-Fixture": "retained", "Content-Type": "application/zip" });
  t.mock.method(globalThis, "fetch", async (_path: string, init: RequestInit) => {
    assert.equal(init.credentials, "same-origin");
    assert.equal(init.cache, "no-store");
    assert.equal(init.signal, controller.signal);
    const sent = new Headers(init.headers);
    assert.equal(sent.get("X-Fixture"), "retained");
    assert.equal(sent.get("Content-Type"), "application/zip");
    assert.equal(sent.get("X-CSRF-Token"), "fixture");
    return new Response(new Uint8Array([0, 255, 80, 75]));
  });
  const response = await request(
    "/api/export",
    { headers, signal: controller.signal, body: "fixture", method: "POST" },
    "fixture",
  );
  assert.deepEqual(new Uint8Array(await response.arrayBuffer()), new Uint8Array([0, 255, 80, 75]));
  assert.equal(headers.has("X-CSRF-Token"), false);
});
