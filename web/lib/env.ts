import "server-only";

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const requiredNames = [
  "PAYD_BASE_URL",
  "PAYD_API_KEY",
  "DASH_PASSWORD_HASH",
  "DASH_TOTP_SECRET",
  "SESSION_SECRET",
  "SESSION_TTL_SECONDS",
  // Not a secret, but still server-only: everything reaches the browser by being
  // passed down deliberately, never by a NEXT_PUBLIC_ prefix (WST-020). Required
  // rather than defaulted, because a dashboard that silently assumes mainnet looks
  // identical on Nile, and that is how a real payout gets made in the belief that
  // it is a test (UI-033, WSYS-054).
  "TRONSCAN_BASE_URL",
] as const;

type RequiredName = (typeof requiredNames)[number];

function required(name: RequiredName): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`Missing required environment variable: ${name}`);
  return value;
}

function appearsInRepository(value: string, directory = process.cwd()): boolean {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if ([".git", ".next", "node_modules"].includes(entry.name)) continue;
    if (entry.isDirectory() && appearsInRepository(value, join(directory, entry.name))) return true;
    if (!entry.isFile() || (entry.name.startsWith(".env") && entry.name !== ".env.example")) continue;
    if (readFileSync(join(directory, entry.name), "utf8").includes(value)) return true;
  }
  return false;
}

export function getEnv() {
  const sessionSecret = required("SESSION_SECRET");
  if (Buffer.byteLength(sessionSecret, "utf8") < 32) throw new Error("SESSION_SECRET must be at least 32 bytes");
  if (appearsInRepository(sessionSecret)) throw new Error("SESSION_SECRET must not appear in the repository");

  const ttl = required("SESSION_TTL_SECONDS");
  if (!/^[1-9][0-9]*$/.test(ttl)) throw new Error("SESSION_TTL_SECONDS must be a positive integer");

  const baseURL = required("PAYD_BASE_URL");
  const host = new URL(baseURL).hostname;
  if (!["127.0.0.1", "::1", "localhost"].includes(host)) throw new Error("PAYD_BASE_URL must use a loopback host");

  const dashboardTotp = required("DASH_TOTP_SECRET");
  for (const [name, value] of Object.entries(process.env)) {
    if (/^PAYD_.*(?:TOTP|SECRET)$/i.test(name) && value === dashboardTotp) {
      throw new Error("DASH_TOTP_SECRET must differ from payd secrets available to this process");
    }
  }

  const tronscan = required("TRONSCAN_BASE_URL");
  if (!/^https:\/\/[^/]+$/.test(tronscan)) {
    throw new Error("TRONSCAN_BASE_URL must be an https origin with no trailing path");
  }

  return {
    TRONSCAN_BASE_URL: tronscan,
    PAYD_BASE_URL: baseURL,
    PAYD_API_KEY: required("PAYD_API_KEY"),
    DASH_PASSWORD_HASH: required("DASH_PASSWORD_HASH"),
    DASH_TOTP_SECRET: dashboardTotp,
    SESSION_SECRET: sessionSecret,
    SESSION_TTL_SECONDS: ttl,
  } as const;
}
