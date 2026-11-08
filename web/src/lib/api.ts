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
    const apiErr = new ApiError(res.status, err?.code ?? "internal_error", err?.message ?? res.statusText, err?.field)
    notifyGlobalError(apiErr)
    throw apiErr
  }
  return data as T
}

/** Name of the window event fired when the backend demands 2FA enrolment. */
export const TOTP_REQUIRED_EVENT = "homepanel:totp-required"

function notifyGlobalError(e: ApiError) {
  if (e.code === "totp_required" && typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent(TOTP_REQUIRED_EVENT))
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

/** Base URL for same-origin links that bypass the JSON wrapper (downloads). */
export const apiBase = "/api/v1"

/** Sends a non-JSON body (text, FormData) with the CSRF header; returns the parsed JSON (or undefined on 204). */
export async function apiRaw<T>(method: Method, path: string, body: BodyInit, contentType?: string): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" }
  if (contentType) headers["Content-Type"] = contentType
  if (csrfToken) headers["X-CSRF-Token"] = csrfToken
  let res: Response
  try {
    res = await fetch(`${apiBase}${path}`, { method, headers, body, credentials: "same-origin" })
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

/** Fetches a plain-text response (file content). */
export async function apiText(path: string): Promise<string> {
  let res: Response
  try {
    res = await fetch(`${apiBase}${path}`, { headers: { Accept: "text/plain" }, credentials: "same-origin" })
  } catch {
    throw new ApiError(0, "network", "network error")
  }
  if (!res.ok) {
    let code = "internal_error"
    let message = res.statusText
    try {
      const data = (await res.json()) as { error?: { code?: string; message?: string } }
      code = data.error?.code ?? code
      message = data.error?.message ?? message
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, code, message)
  }
  return res.text()
}

/** Current CSRF token, for XHR uploads that need progress events. */
export function getCsrfToken() {
  return csrfToken
}
