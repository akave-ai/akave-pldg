'use client';

import Link from 'next/link';

export default function LogsPageHeader() {
  return (
    <header className="shrink-0 border-b border-[var(--border)] bg-[var(--card)]/60 backdrop-blur px-4 py-2.5 flex items-center gap-4 flex-wrap">
      <Link href="/" className="text-[var(--muted)] hover:text-[var(--accent)] text-sm transition-colors">
        ← Home
      </Link>
      <span className="text-[var(--accent)] font-semibold tracking-wide text-sm">LOG EXPLORER</span>
      <div className="ml-auto flex items-center gap-3 text-xs text-[var(--muted)]">
        <Link href="/uploads" className="hover:text-[var(--accent)] transition-colors">
          O3 Uploads
        </Link>
      </div>
    </header>
  );
}

