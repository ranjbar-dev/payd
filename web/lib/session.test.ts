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

const { createSession, invalidateSession, verifyCsrf, verifySession } = await import("./session.ts");

test("encrypted signed session rejects tampering", () => {
  const fresh = createSession(1_000);
  assert.equal(verifySession(fresh.value, 1_001)?.id, fresh.session.id);
  assert.equal(verifySession(`${fresh.value.slice(0, -1)}x`, 1_001), null);
});

test("session has absolute expiry and server invalidation", () => {
  const fresh = createSession(2_000);
  assert.equal(verifySession(fresh.value, fresh.session.exp), null);
  const active = createSession(3_000);
  invalidateSession(active.session.id);
  assert.equal(verifySession(active.value, 3_001), null);
});

test("csrf is bound to the active session", () => {
  const fresh = createSession(4_000);
  const session = verifySession(fresh.value, 4_001);
  assert.ok(session);
  assert.equal(verifyCsrf(session, fresh.csrf, fresh.csrf), true);
  assert.equal(verifyCsrf(session, fresh.csrf, "wrong"), false);
});
