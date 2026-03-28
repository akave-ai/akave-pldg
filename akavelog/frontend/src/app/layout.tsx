import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'Akavelog Demo',
  description: 'Create HTTP inputs, view incoming logs, and monitor uploads to Akave O3',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="min-h-screen antialiased">{children}</body>
    </html>
  );
}
