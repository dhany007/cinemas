"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { ApiError, apiRequest } from "@/lib/api";

type Movie = { id: string; title: string; duration_minutes: number; rating?: string; synopsis?: string; release_date?: string };
export default function MoviesPage() {
  const [movies, setMovies] = useState<Movie[]>([]); const [error, setError] = useState(""); const [loading, setLoading] = useState(true);
  const load = () => { setLoading(true); setError(""); apiRequest<{ movies: Movie[] }>("/movies").then((r) => setMovies(r.movies)).catch((e: unknown) => setError(e instanceof ApiError ? e.message : "Katalog tidak dapat dimuat.")).finally(() => setLoading(false)); };
  useEffect(load, []);
  return <section className="stack" aria-labelledby="movies-title"><div><h1 id="movies-title">Film yang sedang tayang</h1><p className="muted">Pilih film untuk melihat jadwal dan kursi yang tersedia.</p></div>{loading ? <p aria-busy="true">Memuat katalog…</p> : error ? <div className="error" role="alert">{error} <button className="secondary" onClick={load}>Coba lagi</button></div> : movies.length === 0 ? <p className="notice">Belum ada film yang tersedia.</p> : <div className="grid">{movies.map((movie) => <article className="card" key={movie.id}><h2>{movie.title}</h2><p>{movie.duration_minutes} menit{movie.rating ? ` · ${movie.rating}` : ""}</p>{movie.synopsis && <p className="muted">{movie.synopsis}</p>}<Link className="button" href={`/showtimes?movieId=${movie.id}`}>Lihat jadwal</Link></article>)}</div>}</section>;
}
