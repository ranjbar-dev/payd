"use client";

import { AlertTriangle, ArrowLeft } from "lucide-react";

export default function DashboardError() {
  return <main className="page">
    <header><p className="page-kicker">payd / Page unavailable</p><h1 className="page-title mt-2">This page could not be displayed</h1></header>
    <section className="card max-w-2xl space-y-4" role="alert">
      <AlertTriangle aria-hidden="true" size={20} strokeWidth={1.75} className="text-severity-warning" />
      <p className="text-sm leading-relaxed text-ink-secondary">An unexpected error interrupted this view. If you had just submitted an action, check the record’s current state before taking further action.</p>
      <a href="/" className="btn btn-secondary"><ArrowLeft aria-hidden="true" size={14} strokeWidth={1.75} />Return to overview</a>
    </section>
  </main>;
}
