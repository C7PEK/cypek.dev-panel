// Thin fetch wrapper for /api/v1. Errors arrive as ApiError with the server's
// stable error code, which the UI maps to translated messages.

export class ApiError extends Error {
  status: number
  code: string
  field?: string

  constructor(status: number, code: string, message: string, field?: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
    this.field = field
  }
}

let csrfToken = ""

export function setCsrfToken(token: string) {
  csrfToken = token
}

type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE"

export async function api<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  if (method !== "GET" && csrfToken) headers["X-CSRF-Token"] = csrfToken

  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "same-origin",
    })
  } catch {
    throw new ApiError(0, "network", "network error")
  }

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      throw new ApiError(res.status, "bad_response", "invalid JSON from server")
    }
  }

  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string; field?: string } } | null)?.error
    throw new ApiError(res.status, err?.code ?? "internal_error", err?.message ?? res.statusText, err?.field)
  }
  return data as T
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}
