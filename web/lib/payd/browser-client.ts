"use client";

import type { ApiError } from "./types";

export class PaydError extends Error {
  constructor(readonly status: number, readonly code: ApiError["error"]["code"], readonly details: ApiError["error"]["details"]) {
    super(code);
    this.name = "PaydError";
  }
}

export function isPaydError(value: unknown): value is PaydError {
  return value instanceof PaydError;
}

type ErrorEnvelope = { error: Omit<ApiError["error"], "details"> & { details?: ApiError["error"]["details"] } };

function errorBody(value: unknown): ErrorEnvelope | null {
  if (typeof value !== "object" || value === null || !("error" in value)) return null;
  const error = value.error;
  if (typeof error !== "object" || error === null || !("code" in error) || !("message" in error)) return null;
  if (typeof error.code !== "string" || typeof error.message !== "string") return null;
  if ("details" in error && (typeof error.details !== "object" || error.details === null || Array.isArray(error.details))) return null;
  return value as ErrorEnvelope;
}

function csrfToken(): string | undefined {
  return document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith("payd_csrf="))?.slice("payd_csrf=".length);
}

export async function paydRequest<T>(path: readonly string[], init: RequestInit = {}, query?: URLSearchParams, acceptStatus: readonly number[] = []): Promise<T> {
  const method = init.method?.toUpperCase() ?? "GET";
  const headers = new Headers(init.headers);
  if (method === "POST") {
    const token = csrfToken();
    if (token) headers.set("x-csrf-token", token);
  }
  const response = await fetch(`/api/payd/${path.map(encodeURIComponent).join("/")}${query ? `?${query}` : ""}`, {
    ...init,
    headers,
    cache: "no-store",
    credentials: "same-origin",
  });
  const body: unknown = await response.json().catch(() => null);
  const error = errorBody(body);
  if (!response.ok && !acceptStatus.includes(response.status)) throw new PaydError(response.status, error?.error.code ?? "upstream_unreachable", error?.error.details ?? {});
  return body as T;
}
