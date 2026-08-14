"use client";

import { useEffect, useState } from "react";

type TimestampVariant = "default" | "duration" | "utc-day";

function localTime(seconds: number) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(new Date(seconds * 1000));
}

function duration(seconds: number) {
  const sign = seconds < 0 ? "-" : "";
  const absolute = Math.abs(Math.trunc(seconds));
  const hours = Math.floor(absolute / 3600);
  const minutes = Math.floor((absolute % 3600) / 60);
  const remainder = absolute % 60;
  return `${sign}${hours ? `${hours}h ` : ""}${minutes ? `${minutes}m ` : ""}${remainder}s`.trim();
}

export function Timestamp({
  seconds,
  variant = "default",
}: Readonly<{
  seconds: number | null | undefined;
  variant?: TimestampVariant;
}>) {
  const [now, setNow] = useState<number | null>(null);

  useEffect(() => {
    setNow(Date.now());
  }, []);

  if (seconds == null) return <span className="text-ink-faint">—</span>;
  if (variant === "duration")
    return <span className="font-mono tabular-nums">{duration(seconds)}</span>;

  const date = new Date(seconds * 1000);
  const utc = date.toISOString().replace("T", " ").replace(".000Z", " UTC");
  if (variant === "utc-day") return <span title={utc}>{utc}</span>;

  const age = now == null ? null : Math.floor((now - date.getTime()) / 60000);
  const display =
    age != null && age >= 0 && age < 60 ? `${age}m ago` : localTime(seconds);
  return (
    <time dateTime={date.toISOString()} title={utc}>
      {display}
    </time>
  );
}
