export const proxyBase = "/api/backend";

export class ApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
  }
}

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${proxyBase}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init.headers },
  });
  const body: unknown = response.status === 204 ? undefined : await response.json().catch(() => undefined);
  if (!response.ok) {
    const message = isError(body) ? body.message : "Permintaan tidak dapat diproses.";
    throw new ApiError(response.status, message);
  }
  return body as T;
}

function isError(value: unknown): value is { message: string } {
  return typeof value === "object" && value !== null && "message" in value && typeof value.message === "string";
}
