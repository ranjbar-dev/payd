// Local dev auth helper for the payd dashboard. NOT used at runtime.
// Produces the credential material app/api/auth/login/route.ts expects, and
// live TOTP codes for driving login and the payd TOTP-gated actions in tests.
//
//   node scripts/dev-auth.mjs hash '<password>'   -> DASH_PASSWORD_HASH (argon2id PHC)
//   node scripts/dev-auth.mjs base32              -> a fresh base32 secret (DASH_TOTP_SECRET)
//   node scripts/dev-auth.mjs session-secret      -> a 48-byte base64 SESSION_SECRET
//   node scripts/dev-auth.mjs totp '<base32>'     -> current 6-digit TOTP code for that secret

import { argon2, createHmac, randomBytes } from "node:crypto";

const B32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function base32Encode(buf) {
  let bits = "", out = "";
  for (const byte of buf) bits += byte.toString(2).padStart(8, "0");
  for (let i = 0; i + 5 <= bits.length; i += 5) out += B32[parseInt(bits.slice(i, i + 5), 2)];
  return out;
}

function base32Decode(value) {
  let bits = "";
  for (const ch of value.toUpperCase().replace(/=+$/, "")) {
    const i = B32.indexOf(ch);
    if (i === -1) throw new Error("not base32");
    bits += i.toString(2).padStart(5, "0");
  }
  const bytes = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2));
  return Buffer.from(bytes);
}

function totp(secret, now = Date.now()) {
  const step = Math.floor(now / 30000);
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(step));
  const digest = createHmac("sha1", base32Decode(secret)).update(counter).digest();
  const offset = digest[digest.length - 1] & 15;
  const value = ((digest[offset] & 127) << 24) | (digest[offset + 1] << 16) | (digest[offset + 2] << 8) | digest[offset + 3];
  return String(value % 1_000_000).padStart(6, "0");
}

function hash(password) {
  const salt = randomBytes(16);
  return new Promise((resolve, reject) => {
    argon2("argon2id", { message: Buffer.from(password), nonce: salt, memory: 65536, passes: 3, parallelism: 2, tagLength: 32 },
      (err, tag) => err ? reject(err) : resolve(
        `$argon2id$v=19$m=65536,t=3,p=2$${salt.toString("base64").replace(/=+$/, "")}$${Buffer.from(tag).toString("base64").replace(/=+$/, "")}`));
  });
}

const [cmd, arg] = process.argv.slice(2);
if (cmd === "hash") process.stdout.write(await hash(arg));
else if (cmd === "base32") process.stdout.write(base32Encode(randomBytes(20)));
else if (cmd === "session-secret") process.stdout.write(randomBytes(48).toString("base64"));
else if (cmd === "totp") process.stdout.write(totp(arg));
else { console.error("usage: hash|base32|session-secret|totp"); process.exit(1); }
process.stdout.write("\n");
