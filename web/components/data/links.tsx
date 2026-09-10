"use client";

import { Check, Copy, ExternalLink } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { cn } from "@/lib/utils";

function truncate(value: string, start = 6, end = 4) {
  return value.length > start + end + 1
    ? `${value.slice(0, start)}…${value.slice(-end)}`
    : value;
}

function CopyButton({ value }: Readonly<{ value: string }>) {
  const [feedback, setFeedback] = useState("");
  const timeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timeout.current) clearTimeout(timeout.current); }, []);
  const copy = async () => {
    try { await navigator.clipboard.writeText(value); setFeedback("Copied"); }
    catch { setFeedback("Copy unavailable. Select the full value to copy manually."); }
    if (timeout.current) clearTimeout(timeout.current);
    timeout.current = setTimeout(() => setFeedback(""), 3000);
  };
  return (
    <>
    <button
      type="button"
      className="btn btn-ghost ml-1 h-auto px-1 align-middle"
      aria-label={feedback || "Copy full value"}
      title={feedback || "Copy full value"}
      onClick={(event) => { event.preventDefault(); event.stopPropagation(); void copy(); }}
    >
      {feedback === "Copied" ? <Check aria-hidden="true" size={14} strokeWidth={1.75} className="text-severity-success" /> : <Copy aria-hidden="true" size={14} strokeWidth={1.75} />}
    </button>
    <span role="status" className="sr-only">{feedback}</span>
    </>
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
