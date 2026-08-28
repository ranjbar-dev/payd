const required = [
  "PAYD_BASE_URL",
  "PAYD_API_KEY",
  "DASH_PASSWORD_HASH",
  "DASH_TOTP_SECRET",
  "SESSION_SECRET",
  "SESSION_TTL_SECONDS",
];

for (const name of required) {
  if (!process.env[name]?.trim()) throw new Error(`Missing required environment variable: ${name}`);
}

if (Buffer.byteLength(process.env.SESSION_SECRET, "utf8") < 32) {
  throw new Error("SESSION_SECRET must be at least 32 bytes");
}

function appearsInRepository(value, directory = process.cwd()) {
  return readdirSync(directory, { withFileTypes: true }).some((entry) => {
    if ([".git", ".next", "node_modules"].includes(entry.name)) return false;
    if (entry.isDirectory()) return appearsInRepository(value, join(directory, entry.name));
    return entry.isFile() && (!entry.name.startsWith(".env") || entry.name === ".env.example") && readFileSync(join(directory, entry.name), "utf8").includes(value);
  });
}

if (appearsInRepository(process.env.SESSION_SECRET)) {
  throw new Error("SESSION_SECRET must not appear in the repository");
}

/** @type {import('next').NextConfig} */
const nextConfig = {};

export default nextConfig;
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
