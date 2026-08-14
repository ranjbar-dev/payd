import "server-only";

import { createCipheriv, createDecipheriv, createHmac, createHash, randomBytes, timingSafeEqual } from "node:crypto";

import { getEnv } from "./env.ts";

const activeSessions = new Map<string, number>();
const sessionWhoami = new Map<string, { keyName: string; scopes: string[] }>();

export type Session = { iat: number; exp: number; id: string };
export type NewSession = { value: string; csrf: string; session: Session };

function key(): Buffer {
  return createHash("sha256").update(getEnv().SESSION_SECRET).digest();
}

function base64url(value: Buffer): string {
  return value.toString("base64url");
}

function equal(left: string, right: string): boolean {
  const a = Buffer.from(left);
  const b = Buffer.from(right);
  return a.length === b.length && timingSafeEqual(a, b);
}

function csrfFor(id: string): string {
  return base64url(createHmac("sha256", key()).update(`csrf:${id}`).digest());
}

function isSession(value: unknown): value is Session {
  if (typeof value !== "object" || value === null) return false;
  const session = value as Record<string, unknown>;
  return Object.keys(session).length === 3
    && typeof session.iat === "number"
    && typeof session.exp === "number"
    && typeof session.id === "string"
    && /^[A-Za-z0-9_-]{32,}$/.test(session.id);
}

function prune(now = Date.now()): void {
  for (const [id, expiry] of activeSessions) if (expiry <= now) activeSessions.delete(id);
}

export function createSession(now = Date.now()): NewSession {
  prune(now);
  const session: Session = {
    iat: now,
    exp: now + Number(getEnv().SESSION_TTL_SECONDS) * 1000,
    id: base64url(randomBytes(32)),
  };
  const iv = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", key(), iv);
  const ciphertext = Buffer.concat([cipher.update(JSON.stringify(session), "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();
  activeSessions.set(session.id, session.exp);
  return { value: `${base64url(iv)}.${base64url(ciphertext)}.${base64url(tag)}`, csrf: csrfFor(session.id), session };
}

export function verifySession(value: string | undefined, now = Date.now()): Session | null {
  if (!value) return null;
  const parts = value.split(".");
  if (parts.length !== 3) return null;
  try {
    const [iv, ciphertext, tag] = parts.map((part) => Buffer.from(part, "base64url"));
    if (iv.length !== 12 || tag.length !== 16) return null;
    const decipher = createDecipheriv("aes-256-gcm", key(), iv);
    decipher.setAuthTag(tag);
    const session: unknown = JSON.parse(Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString("utf8"));
    prune(now);
    if (!isSession(session) || session.iat > now || session.exp <= now || activeSessions.get(session.id) !== session.exp) return null;
    return session;
  } catch {
    return null;
  }
}

export function verifyCsrf(session: Session, cookie: string | undefined, header: string | null): boolean {
  const expected = csrfFor(session.id);
  return Boolean(cookie && header && equal(cookie, expected) && equal(header, expected));
}

export function invalidateSession(id: string): void {
  activeSessions.delete(id);
  sessionWhoami.delete(id);
}

export function setSessionWhoami(id: string, keyName: string, scopes: string[]): void {
  if (activeSessions.has(id)) sessionWhoami.set(id, { keyName, scopes: [...scopes] });
}

export function getSessionWhoami(id: string): { keyName: string; scopes: string[] } | null {
  return sessionWhoami.get(id) ?? null;
}
