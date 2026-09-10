import { ArrowLeft, FileQuestion } from "lucide-react";
import Link from "next/link";

export default function NotFound() {
  return <main className="grid min-h-screen place-items-center p-4">
    <section className="w-full max-w-lg border-t border-border-strong py-8">
      <FileQuestion aria-hidden="true" size={24} strokeWidth={1.75} className="mb-4 text-ink-secondary" />
      <p className="page-kicker">payd / 404</p>
      <h1 className="page-title mt-2">Page not found</h1>
      <p className="mt-3 text-sm leading-relaxed text-ink-secondary">This address does not match a dashboard page. Check the URL or return to the overview to find the record you need.</p>
      <Link href="/" className="btn btn-primary mt-6"><ArrowLeft aria-hidden="true" size={14} strokeWidth={1.75} />Return to overview</Link>
    </section>
  </main>;
}
