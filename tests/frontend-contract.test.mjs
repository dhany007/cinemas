import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const requiredPaths = [
  "/v1/movies",
  "/v1/movies/{movieID}/showtimes",
  "/v1/showtimes/{showtimeID}/seats",
  "/v1/orders",
  "/v1/orders/{orderID}/tickets",
  "/v1/admin/cinemas",
  "/v1/admin/movies",
  "/v1/admin/showtimes",
  "/v1/admin/tickets/{qrToken}/check-in",
];

test("frontend journeys are backed by documented protected API paths", async () => {
  const openAPI = await readFile(new URL("../openapi/openapi.yaml", import.meta.url), "utf8");
  for (const path of requiredPaths) {
    assert.match(openAPI, new RegExp(`^  ${escapeRegExp(path)}:`, "m"));
  }
});

test("browser clients use their app-local BFF proxy", async () => {
  for (const app of ["customer-web", "admin-web"]) {
    const source = await readFile(new URL(`../apps/${app}/lib/api.ts`, import.meta.url), "utf8");
    assert.match(source, /const proxyBase = "\/api\/backend"/);
    assert.doesNotMatch(source, /API_BASE_URL/);
    assert.doesNotMatch(source, /Authorization/);
  }
});

test("BFF login keeps the access token in an HTTP-only cookie", async () => {
  for (const app of ["customer-web", "admin-web"]) {
    const source = await readFile(new URL(`../apps/${app}/app/api/session/route.ts`, import.meta.url), "utf8");
    assert.match(source, /NextResponse\.json\(\{ user: body\.user \}\)/);
    assert.match(source, /httpOnly: true/);
    assert.match(source, /const result = NextResponse\.json\(\{ user: body\.user \}\)/);
  }
});

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
