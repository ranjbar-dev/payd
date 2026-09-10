"use client";

import { AlertTriangle, KeyRound, Loader2, Lock, LogIn } from "lucide-react";
import { FormEvent, useState } from "react";

export default function LoginPage() {
  const [message, setMessage] = useState("");
  const [pending, setPending] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setPending(true);
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ password: form.get("password"), dashboard_code: form.get("dashboard_code") }),
    });
    if (response.ok) window.location.assign("/");
    else {
      setMessage(response.status === 429 ? "Too many attempts. Try again later." : response.status >= 500 ? "The payment service is unavailable. A session could not be started. Check the service connection before signing in again." : "invalid credentials");
      setPending(false);
    }
  }

  return <main className="grid min-h-screen place-items-center bg-canvas p-6">
    <form className="card grid w-full max-w-sm gap-5 p-6 sm:p-8" onSubmit={submit}>
      <div className="border-b border-border-subtle pb-5">
        <p className="text-[22px] font-semibold tracking-tight text-ink">payd<span className="text-accent">.</span></p>
        <p className="mt-1 text-xs text-ink-faint">Operations console</p>
      </div>
      <div><h1 className="page-title">Sign in</h1><p className="mt-2 text-sm leading-relaxed text-ink-secondary">Enter your password and the six-digit code from your dashboard authenticator.</p></div>
      <label className="field">
        Password
        <span className="relative">
          <Lock aria-hidden size={14} strokeWidth={1.75} className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-ink-faint" />
          <input className="input pl-8" name="password" type="password" autoComplete="current-password" disabled={pending} required />
        </span>
      </label>
      <label className="field">
        Dashboard code
        <span className="relative">
          <KeyRound aria-hidden size={14} strokeWidth={1.75} className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-ink-faint" />
          <input className="input pl-8 font-mono tracking-widest" name="dashboard_code" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} autoComplete="one-time-code" disabled={pending} required />
        </span>
      </label>
      <button className="btn btn-primary w-full" type="submit" disabled={pending}>
        {pending ? <Loader2 aria-hidden size={14} strokeWidth={1.75} className="animate-spin" /> : <LogIn aria-hidden size={14} strokeWidth={1.75} />}
        {pending ? "Signing in…" : "Sign in"}
      </button>
      {message && <p role="alert" className="flex items-center gap-1.5 text-[13px] text-severity-critical"><AlertTriangle aria-hidden size={14} strokeWidth={1.75} />{message}</p>}
    </form>
  </main>;
}
