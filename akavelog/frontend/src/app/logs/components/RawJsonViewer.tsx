'use client';

import React from 'react';

type Props = {
  content: string;
  wrap: boolean;
  className?: string;
};

const TOKEN_RE =
  /("(?:\\u[a-fA-F0-9]{4}|\\[^u]|[^\\"])*"(?=\s*:)?|"(?:\\u[a-fA-F0-9]{4}|\\[^u]|[^\\"])*"|\btrue\b|\bfalse\b|\bnull\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;

function tokenClass(token: string): string {
  if (token === 'true' || token === 'false') return 'text-cyan-300';
  if (token === 'null') return 'text-zinc-400';
  if (/^-?\d/.test(token)) return 'text-amber-300';
  if (/^".*":$/.test(token)) return 'text-purple-300';
  if (/^"/.test(token)) return 'text-emerald-300';
  return 'text-[var(--text)]';
}

export default function RawJsonViewer({ content, wrap, className }: Props) {
  const parts: React.ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  TOKEN_RE.lastIndex = 0;
  while ((m = TOKEN_RE.exec(content)) !== null) {
    if (m.index > last) parts.push(content.slice(last, m.index));
    parts.push(
      <span key={`${m.index}-${m[0]}`} className={tokenClass(m[0])}>
        {m[0]}
      </span>
    );
    last = m.index + m[0].length;
  }
  if (last < content.length) parts.push(content.slice(last));

  return (
    <pre
      className={`text-xs font-mono overflow-auto max-h-[420px] p-3 rounded border border-[var(--border)] bg-[var(--bg)] text-[var(--text)] ${
        wrap ? 'whitespace-pre-wrap break-words' : 'whitespace-pre'
      } ${className || ''}`}
    >
      {parts}
    </pre>
  );
}

