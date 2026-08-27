import { cookies } from "next/headers";
import { NextResponse } from "next/server";

const cookieName = "cinemas_access_token";
const backendBase = process.env.API_BASE_URL ?? "http://127.0.0.1:18081";

type RouteContext = { params: Promise<{ path: string[] }> };

async function proxy(request: Request, context: RouteContext) {
  const { path } = await context.params;
  const headers = new Headers();
  const contentType = request.headers.get("content-type");
  const idempotencyKey = request.headers.get("idempotency-key");
  if (contentType) headers.set("content-type", contentType);
  if (idempotencyKey) headers.set("idempotency-key", idempotencyKey);
  const token = (await cookies()).get(cookieName)?.value;
  if (token) headers.set("authorization", `Bearer ${token}`);
  const method = request.method;
  const body = method === "GET" || method === "HEAD" ? undefined : await request.arrayBuffer();
  const response = await fetch(`${backendBase}/v1/${path.join("/")}${new URL(request.url).search}`, {
    method,
    headers,
    body,
    cache: "no-store",
  });
  const responseBody = await response.arrayBuffer();
  const output = new NextResponse(responseBody, { status: response.status });
  const responseType = response.headers.get("content-type");
  if (responseType) output.headers.set("content-type", responseType);
  return output;
}

export const GET = proxy;
export const POST = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
