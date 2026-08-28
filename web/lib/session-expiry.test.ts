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

const { createSession, verifySession } = await import("./session.ts");
const { proxyPaydRequest } = await import("../app/api/payd/[...path]/route.ts");

test("G1-6 rejects expired and tampered sessions before contacting payd", async () => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => { calls += 1; return new Response("unexpected"); };
  try {
    const expired = createSession(0);
    // Flip a bit in the decoded GCM tag rather than swapping the last base64url
    // character. base64url's final character carries bits that decoding discards,
    // so `slice(0, -1) + "x"` can decode to the SAME 16 bytes and leave the
    // session perfectly valid — which made this assertion pass or fail depending
    // on what the last character happened to be.
    const [iv, ciphertext, tag] = createSession().value.split(".");
    const forged = Buffer.from(tag, "base64url");
    forged[0] ^= 0xff;
    const tampered = `${iv}.${ciphertext}.${forged.toString("base64url")}`;
    assert.equal(verifySession(expired.value, expired.session.exp), null);
    assert.equal(verifySession(tampered), null);
    for (const value of [expired.value, tampered]) {
      const response = await proxyPaydRequest(new Request("http://dashboard.test/api/payd/orders", {
        method: "POST", headers: { cookie: `payd_session=${value}` }, body: "{}",
      }), { params: Promise.resolve({ path: ["orders"] }) });
      assert.equal(response.status, 401);
    }
    assert.equal(calls, 0, "invalid sessions must not reach payd");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
