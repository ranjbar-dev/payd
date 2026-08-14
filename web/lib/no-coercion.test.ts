import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";
import ts from "typescript";

const moneyNames = [
  "amount", "amount_raw", "amount_usd", "confirmed", "confirmed_raw", "pending", "chain_raw", "usd", "balance", "fee_raw",
  "total_cost_trx", "energy_cost_trx", "bandwidth_cost_trx", "network_fee_trx", "resource_fee_trx", "used_usd", "remaining_usd",
  "daily_limit_usd", "price_usd", "min_deposit",
].join("|");
const money = new RegExp(`^(?:${moneyNames})$`);
const binaryOperators = new Set([
  ts.SyntaxKind.PlusToken, ts.SyntaxKind.MinusToken, ts.SyntaxKind.AsteriskToken, ts.SyntaxKind.SlashToken, ts.SyntaxKind.PercentToken,
  ts.SyntaxKind.LessThanToken, ts.SyntaxKind.LessThanEqualsToken, ts.SyntaxKind.GreaterThanToken, ts.SyntaxKind.GreaterThanEqualsToken,
  ts.SyntaxKind.EqualsEqualsToken, ts.SyntaxKind.EqualsEqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsToken, ts.SyntaxKind.ExclamationEqualsEqualsToken,
]);

type Hit = { file: string; line: number; source: string };

// Exact source lines only. They convert Unix timestamps (not money) or set session TTL.
const allowed = new Map<string, string>([
  ["app/(dash)/orders-dashboard.tsx:return seconds && /^\\d+$/.test(seconds) ? new Date(Number(seconds) * 1000).toISOString().slice(0, 10) : \"\";", "UTC date filter"],
  ["app/(dash)/payments-dashboard.tsx:const date = new Date(Number(seconds) * 1000);", "local date filter"],
  ["app/(dash)/payments-dashboard.tsx:const resolvedRange = filters.from || filters.to ? <p className=\"text-xs text-ink-secondary\">Block range (UTC): {filters.from ? <Timestamp seconds={Number(filters.from)} variant=\"utc-day\" /> : \"unbounded\"} to {filters.to ? <Timestamp seconds={Number(filters.to)} variant=\"utc-day\" /> : \"unbounded\"}, inclusive.</p> : null;", "UTC date filter"],
  ["lib/session.ts:exp: now + Number(getEnv().SESSION_TTL_SECONDS) * 1000,", "session TTL milliseconds"],
]);

function detect(source: string, file = "inline.ts"): Hit[] {
  const sourceFile = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const lines = source.split(/\r?\n/);
  const hits: Hit[] = [];
  const add = (node: ts.Node) => {
    const line = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line;
    hits.push({ file, line: line + 1, source: lines[line].trim() });
  };
  const isMoney = (node: ts.Expression): boolean => {
    if (ts.isIdentifier(node)) return money.test(node.text);
    return ts.isPropertyAccessExpression(node) && money.test(node.name.text);
  };
  const visit = (node: ts.Node): void => {
    if (ts.isCallExpression(node)) {
      const callee = node.expression;
      if ((ts.isIdentifier(callee) && ["Number", "parseFloat", "parseInt"].includes(callee.text))
        || (ts.isPropertyAccessExpression(callee) && ["toFixed", "toLocaleString"].includes(callee.name.text))) add(node);
    }
    if (ts.isBinaryExpression(node) && binaryOperators.has(node.operatorToken.kind) && (isMoney(node.left) || isMoney(node.right))) add(node);
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  return hits;
}

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const file = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(file);
    return entry.isFile() && /\.(?:ts|tsx)$/.test(entry.name) && !entry.name.endsWith(".test.ts") ? [file] : [];
  });
}

test("G1-2 detector catches a numeric money coercion", () => {
  assert.equal(detect("const value = Number(w.amount_usd);").length, 1);
  assert.equal(detect("const value = w.amount_usd + 1;").length, 1);
});

test("G1-2 permits only listed timestamp and TTL coercions", () => {
  const hits = ["app", "components", "lib"].flatMap((directory) => sourceFiles(directory).flatMap((file) => {
    const relativeFile = relative(process.cwd(), file).replaceAll("\\", "/");
    return detect(readFileSync(file, "utf8"), relativeFile);
  }));
  const unexpected = hits.filter((hit) => !allowed.has(`${hit.file}:${hit.source}`));
  assert.deepEqual(unexpected, [], `Unexpected numeric coercion:\n${unexpected.map((hit) => `${hit.file}:${hit.line}: ${hit.source}`).join("\n")}`);
  assert.deepEqual(new Set(hits.map((hit) => `${hit.file}:${hit.source}`)), new Set(allowed.keys()), "The coercion allowlist must exactly match source");
});
