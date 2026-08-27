import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";
export const metadata: Metadata = { title: "Cinema operations", description: "Administrative cinema operations" };
export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) { return <html lang="id"><body><header className="site-header"><Link className="brand" href="/">Cinema admin</Link><nav aria-label="Navigasi administrasi"><Link href="/cinemas">Bioskop</Link><Link href="/seat-layout">Layout kursi</Link><Link href="/movies">Film</Link><Link href="/showtimes">Jadwal</Link><Link href="/tickets">Tiket</Link><Link href="/login">Masuk</Link></nav></header><main>{children}</main></body></html>; }
