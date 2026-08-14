import { randomBytes } from "node:crypto";
import assert from "node:assert/strict";
import test from "node:test";

process.env.PAYD_BASE_URL = "http://127.0.0.1:8080";
process.env.PAYD_API_KEY = "test-key";
process.env.DASH_PASSWORD_HASH = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$ZmFrZWhhc2g";
process.env.DASH_TOTP_SECRET = "dashboard-test-secret";
// Random per run, never a fixed literal: `getEnv()` refuses a SESSION_SECRET that
// appears anywhere in the repository, and a constant test secret gets copied into
// spawn logs and CI output, which then fails every later run.
process.env.SESSION_SECRET = randomBytes(32).toString("hex");
process.env.SESSION_TTL_SECONDS = "60";
process.env.TRONSCAN_BASE_URL = "https://tronscan.org";

const { createSession } = await import("./session.ts");
const { proxyPaydRequest } = await import("../app/api/payd/[...path]/route.ts");

test("G1-5 POST calls payd once for timeout, errors, and connection reset", async () => {
  const originalFetch = globalThis.fetch;
  const cases: [string, () => Promise<Response>][] = [
    ["timeout", async () => { throw new DOMException("timed out", "TimeoutError"); }],
    ["500", async () => new Response("server error", { status: 500 })],
    ["502", async () => new Response("bad gateway", { status: 502 })],
    ["429", async () => new Response("rate limited", { status: 429 })],
    ["connection reset", async () => { throw new Error("ECONNRESET"); }],
  ];
  try {
    for (const [name, result] of cases) {
      let calls = 0;
      globalThis.fetch = async () => { calls += 1; return result(); };
      const session = createSession();
      const response = await proxyPaydRequest(new Request("http://dashboard.test/api/payd/orders", {
        method: "POST",
        headers: {
          cookie: `payd_session=${session.value}; payd_csrf=${session.csrf}`,
          "x-csrf-token": session.csrf,
          "idempotency-key": "test-idempotency-key",
          "content-type": "application/json",
        },
        body: "{}",
      }), { params: Promise.resolve({ path: ["orders"] }) });
      assert.equal(calls, 1, `${name} must not cause a re-sent POST`);
      assert.ok(response.status >= 400);
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});
