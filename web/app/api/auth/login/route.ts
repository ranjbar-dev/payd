import { argon2, createHmac, timingSafeEqual } from "node:crypto";

import { getEnv } from "@/lib/env";
import { createSession, invalidateSession, setSessionWhoami } from "@/lib/session";
import { proxyPaydRequest } from "../../payd/[...path]/route";

const attempts = new Map<string, number[]>();
const usedCodes = new Map<string, number>();

function log(action: string, outcome: string, target = "login"): void {
  console.info(JSON.stringify({ timestamp: new Date().toISOString(), action, target, outcome }));
}

function clientIP(request: Request): string {
  return request.headers.get("x-forwarded-for")?.split(",")[0]?.trim() || request.headers.get("x-real-ip") || "unknown";
}

function limited(ip: string, now = Date.now()): boolean {
  const history = (attempts.get(ip) ?? []).filter((at) => at > now - 60 * 60 * 1000);
  if (history.filter((at) => at > now - 60 * 1000).length >= 5 || history.length >= 20) return true;
  history.push(now);
  attempts.set(ip, history);
  return false;
}

function base64(value: string): Buffer {
  return Buffer.from(value + "=".repeat((4 - (value.length % 4)) % 4), "base64");
}

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function base32Decode(value: string): Buffer {
  let bits = "";
  for (const char of value.toUpperCase().replace(/=+$/, "")) {
    const index = BASE32_ALPHABET.indexOf(char);
    if (index === -1) throw new Error("DASH_TOTP_SECRET is not valid base32");
    bits += index.toString(2).padStart(5, "0");
  }
  const bytes: number[] = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2));
  return Buffer.from(bytes);
}

function parseHash(hash: string): { memory: number; passes: number; parallelism: number; salt: Buffer; tag: Buffer } | null {
  const match = /^\$argon2id\$v=19\$m=(\d+),t=(\d+),p=(\d+)\$([A-Za-z0-9+/]+)\$([A-Za-z0-9+/]+)$/.exec(hash);
  if (!match) return null;
  const [memory, passes, parallelism] = match.slice(1, 4).map(Number);
  const salt = base64(match[4]);
  const tag = base64(match[5]);
  if (!Number.isSafeInteger(memory) || !Number.isSafeInteger(passes) || !Number.isSafeInteger(parallelism) || salt.length < 8 || tag.length < 5) return null;
  return { memory, passes, parallelism, salt, tag };
}

async function verifyPassword(password: string): Promise<boolean> {
  const parsed = parseHash(getEnv().DASH_PASSWORD_HASH);
  if (!parsed) return false;
  const actual = await new Promise<Buffer>((resolve, reject) => {
    argon2("argon2id", {
      message: Buffer.from(password), nonce: parsed.salt, memory: parsed.memory,
      passes: parsed.passes, parallelism: parsed.parallelism, tagLength: parsed.tag.length,
    }, (error, derivedKey) => error ? reject(error) : resolve(derivedKey));
  });
  return actual.length === parsed.tag.length && timingSafeEqual(actual, parsed.tag);
}

function totpStep(code: string, step: number): boolean {
  if (!/^\d{6}$/.test(code)) return false;
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(step));
  const digest = createHmac("sha1", base32Decode(getEnv().DASH_TOTP_SECRET)).update(counter).digest();
  const offset = digest[digest.length - 1] & 15;
  const value = ((digest[offset] & 127) << 24) | (digest[offset + 1] << 16) | (digest[offset + 2] << 8) | digest[offset + 3];
  return String(value % 1_000_000).padStart(6, "0") === code;
}

function verifyDashboardCode(code: string, now = Date.now()): { valid: boolean; step: number } {
  const step = Math.floor(now / 30_000);
  return { valid: [step - 1, step, step + 1].some((candidate) => totpStep(code, candidate)), step };
}

function credentialsError(status = 401): Response {
  return Response.json({ error: { code: "invalid_credentials", message: "invalid credentials" } }, { status });
}

export async function POST(request: Request): Promise<Response> {
  const ip = clientIP(request);
  if (limited(ip)) {
    log("login", "rate_limited");
    return Response.json({ error: { code: "rate_limited", message: "Too many attempts" } }, { status: 429 });
  }

  let body: unknown = null;
  try { body = await request.json(); } catch { /* Verify an empty password below too. */ }
  const record = typeof body === "object" && body !== null ? body as Record<string, unknown> : {};
  const password = typeof record.password === "string" ? record.password : "";
  const code = typeof record.dashboard_code === "string" ? record.dashboard_code : "";
  const [passwordValid, codeResult] = await Promise.all([verifyPassword(password), Promise.resolve(verifyDashboardCode(code))]);
  const usedKey = `${code}:${codeResult.step}`;
  const now = Date.now();
  for (const [key, expiry] of usedCodes) if (expiry <= now) usedCodes.delete(key);
  if (!passwordValid || !codeResult.valid || usedCodes.has(usedKey)) {
    log("login", "failure");
    return credentialsError();
  }

  const fresh = createSession(now);
  const whoami = await proxyPaydRequest(new Request(new URL("/api/payd/auth/whoami", request.url), {
    headers: { cookie: `payd_session=${fresh.value}` },
  }), { params: Promise.resolve({ path: ["auth", "whoami"] }) });
  let identity: { key_name: string; scopes: string[] } | null = null;
  try {
    const value: unknown = await whoami.clone().json();
    if (typeof value === "object" && value !== null) {
      const item = value as Record<string, unknown>;
      if (typeof item.key_name === "string" && Array.isArray(item.scopes) && item.scopes.every((scope) => typeof scope === "string")) {
        identity = { key_name: item.key_name, scopes: item.scopes };
      }
    }
  } catch { /* The proxy response remains the only upstream detail. */ }
  if (!whoami.ok || !identity) {
    invalidateSession(fresh.session.id);
    log("login", "upstream_failure");
    return Response.json({ error: { code: "upstream_unavailable", message: "Unable to start session" } }, { status: 503 });
  }

  usedCodes.set(usedKey, now + 90_000);
  setSessionWhoami(fresh.session.id, identity.key_name, identity.scopes);
  const response = Response.json({ ok: true });
  const maxAge = Math.ceil((fresh.session.exp - now) / 1000);
  response.headers.append("Set-Cookie", `payd_session=${fresh.value}; Path=/; Max-Age=${maxAge}; HttpOnly; Secure; SameSite=Strict`);
  response.headers.append("Set-Cookie", `payd_csrf=${fresh.csrf}; Path=/; Max-Age=${maxAge}; Secure; SameSite=Strict`);
  log("login", "success");
  return response;
}
