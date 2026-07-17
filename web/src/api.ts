export interface ChatResponse {
  session_id: string
  answer: string
}

interface ErrorResponse {
  error?: {
    code?: string
    message?: string
  }
}

export class ApiError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

async function readError(response: Response): Promise<string> {
  try {
    const payload = (await response.json()) as ErrorResponse
    if (payload.error?.message) {
      return payload.error.message
    }
  } catch {
    // The fallback below handles non-JSON proxy and network responses.
  }
  return `请求失败（${response.status}）`
}

export async function checkHealth(signal?: AbortSignal): Promise<boolean> {
  try {
    const response = await fetch('/api/health', { signal })
    return response.ok
  } catch {
    return false
  }
}

export async function chat(message: string, sessionID: string | null): Promise<ChatResponse> {
  const response = await fetch('/api/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      session_id: sessionID ?? '',
      message,
    }),
  })
  if (!response.ok) {
    throw new ApiError(await readError(response), response.status)
  }
  return (await response.json()) as ChatResponse
}

export async function resetSession(sessionID: string | null): Promise<void> {
  if (!sessionID) {
    return
  }
  const response = await fetch('/api/sessions/reset', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionID }),
  })
  if (!response.ok) {
    throw new ApiError(await readError(response), response.status)
  }
}
