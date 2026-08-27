import { NextResponse } from "next/server";

const cookieName = "cinemas_access_token";
const backendBase = process.env.API_BASE_URL ?? "http://127.0.0.1:18081";

export async function POST(request: Request) {
  const credentials = await request.json();
  const response = await fetch(`${backendBase}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(credentials),
    cache: "no-store",
  });
  const body = await response.json();
  if (!response.ok) {
    return NextResponse.json(body, { status: response.status });
  }
  const result = NextResponse.json({ user: body.user });
  result.cookies.set(cookieName, body.access_token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
  });
  return result;
}

export async function DELETE() {
  const result = new NextResponse(null, { status: 204 });
  result.cookies.set(cookieName, "", { httpOnly: true, path: "/", maxAge: 0 });
  return result;
}
