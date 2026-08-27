import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";

export const metadata: Metadata = { title: "Cinema tickets", description: "Book cinema tickets securely" };

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="id"><body><header className="site-header"><Link className="brand" href="/">Cinema</Link><nav aria-label="Navigasi utama"><Link href="/showtimes">Jadwal</Link><Link href="/orders">Pesanan saya</Link><Link href="/login">Masuk</Link></nav></header><main>{children}</main></body></html>;
}
