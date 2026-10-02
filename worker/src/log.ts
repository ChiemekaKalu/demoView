// errorText is an error's message, without the "Error: " prefix String adds.
export function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// errorStack is the stack trace when there is one.
export function errorStack(err: unknown): string | undefined {
  return err instanceof Error ? err.stack : undefined;
}
