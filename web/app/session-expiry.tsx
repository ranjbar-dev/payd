"use client";

import { AlertTriangle } from "lucide-react";

import { useSessionExpiry } from "./providers";

function remainingTime(remainingMs: number): string {
  const seconds = Math.ceil(remainingMs / 1_000);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

export function SessionExpiryNotice() {
  const { isExpired, isExpiringSoon, remainingMs } = useSessionExpiry();
  if (!isExpired && !isExpiringSoon) return null;

  if (isExpired) return <section role="alert" className="flex items-start gap-2 border border-severity-critical bg-[var(--severity-critical-bg)] p-3 text-sm text-severity-critical"><AlertTriangle aria-hidden="true" className="mt-0.5 shrink-0" size={16} /><p><strong>Dashboard session expired.</strong> Your in-progress form remains on screen, but requests are no longer authorised. You must log in again before continuing.</p></section>;

  return <section role="alert" className="flex items-start gap-2 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" className="mt-0.5 shrink-0" size={16} /><p><strong>Dashboard session expires in <span className="font-mono tabular-nums">{remainingTime(remainingMs)}</span>.</strong> Save any in-progress form and log in again before it expires.</p></section>;
}
