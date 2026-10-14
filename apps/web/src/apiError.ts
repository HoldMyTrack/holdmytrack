/**
 * What an API error response says (ADR-0045): every error the server sends is JSON
 * `{"error": code, "message": text}` — `code` to branch on, `message` to show. A body that isn't
 * that shape (a proxy's error page in front of the server, an empty body) has no code; its text,
 * or the fallback when there's none, is the message.
 */
export interface ApiErrorBody {
  code: string | null;
  message: string;
}

export function parseErrorBody(text: string, fallback: string): ApiErrorBody {
  if (!text) return { code: null, message: fallback };
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === 'object') {
      const { error, message } = parsed as { error?: unknown; message?: unknown };
      if (typeof error === 'string' || typeof message === 'string') {
        return {
          code: typeof error === 'string' ? error : null,
          message: typeof message === 'string' && message ? message : fallback,
        };
      }
    }
  } catch {
    // Not JSON: someone else's error page, whose text is all there is.
  }
  return { code: null, message: text };
}