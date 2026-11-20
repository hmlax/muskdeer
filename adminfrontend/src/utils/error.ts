/** Read an error message without trusting arbitrary rejection payloads. */
export function getErrorMessage(error: unknown, fallback = ''): string {
  if (error instanceof Error) return error.message || fallback;
  if (
    error !== null &&
    typeof error === 'object' &&
    'message' in error &&
    typeof error.message === 'string'
  )
    return error.message || fallback;
  return fallback;
}
