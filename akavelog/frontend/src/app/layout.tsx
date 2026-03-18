import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'Akavelog',
  description: 'Insert logs, explore them, and view stored data in Akave O3',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="min-h-screen antialiased" suppressHydrationWarning>{children}</body>
    </html>
  );
}
