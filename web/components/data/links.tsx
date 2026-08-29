"use client";

import { Copy, ExternalLink } from "lucide-react";

import { cn } from "@/lib/utils";

function truncate(value: string, start = 6, end = 4) {
  return value.length > start + end + 1
    ? `${value.slice(0, start)}…${value.slice(-end)}`
    : value;
}

function CopyButton({ value }: Readonly<{ value: string }>) {
  return (
    <button
      type="button"
      className="btn btn-ghost ml-1 h-auto px-1 align-middle"
      aria-label="Copy full value"
      title="Copy full value"
      onClick={() => void navigator.clipboard.writeText(value)}
    >
      <Copy aria-hidden="true" size={12} strokeWidth={1.75} />
    </button>
  );
}

export function AddressLink({
  address,
  href,
  className,
}: Readonly<{ address: string; href: string; className?: string }>) {
  return (
    <span
      className={cn("font-mono tabular-nums", className)}
      data-address
      title={address}
    >
      <a
        href={href}
        className="text-ink-secondary underline-offset-2 transition-colors duration-150 hover:text-ink hover:underline"
      >
        {truncate(address)}
      </a>
      <CopyButton value={address} />
    </span>
  );
}

export function TxidLink({
  txid,
  tronscanBaseUrl,
  className,
}: Readonly<{ txid: string; tronscanBaseUrl: string; className?: string }>) {
  const href = `${tronscanBaseUrl.replace(/\/$/, "")}/#/transaction/${encodeURIComponent(txid)}`;
  return (
    <span
      className={cn("font-mono tabular-nums", className)}
      data-txid
      title={txid}
    >
      <span>{truncate(txid)}</span>
      <CopyButton value={txid} />
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className="btn btn-ghost ml-1 h-auto px-1 align-middle"
        aria-label="Open transaction in Tronscan"
        title="Open in Tronscan"
      >
        <ExternalLink aria-hidden="true" size={12} strokeWidth={1.75} />
      </a>
    </span>
  );
}

export function EntityId({
  value,
  className,
  full = false,
}: Readonly<{ value: string; className?: string; full?: boolean }>) {
  return (
    <span
      className={cn("font-mono tabular-nums", className)}
      data-entity-id
      title={value}
    >
      {full ? value : truncate(value, 8, 6)}
      <CopyButton value={value} />
    </span>
  );
}
