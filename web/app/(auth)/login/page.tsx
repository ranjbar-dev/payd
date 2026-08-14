"use client";

import { FormEvent, useState } from "react";

export default function LoginPage() {
  const [message, setMessage] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ password: form.get("password"), dashboard_code: form.get("dashboard_code") }),
    });
    if (response.ok) window.location.assign("/");
    else setMessage(response.status === 429 ? "Too many attempts. Try again later." : "invalid credentials");
  }

  return <main className="grid min-h-screen place-items-center p-6">
    <form className="grid w-full max-w-sm gap-4" onSubmit={submit}>
      <h1 className="text-2xl font-semibold">Sign in</h1>
      <label className="grid gap-1">Password<input name="password" type="password" autoComplete="current-password" required /></label>
      <label className="grid gap-1">Dashboard code<input name="dashboard_code" inputMode="numeric" pattern="[0-9]{6}" autoComplete="one-time-code" required /></label>
      <button type="submit">Sign in</button>
      {message && <p role="alert">{message}</p>}
    </form>
  </main>;
}
