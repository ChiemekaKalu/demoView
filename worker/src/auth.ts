const encoder = new TextEncoder();

// Checks `Authorization: Bearer <UPLOAD_TOKEN>`. Both sides are hashed first
// so the comparison is constant-time regardless of length. An unset token
// rejects everything.
export async function hasUploadToken(request: Request, env: Env): Promise<boolean> {
  const header = request.headers.get("authorization") ?? "";
  const token = /^Bearer (.+)$/.exec(header)?.[1];
  if (!token || !env.UPLOAD_TOKEN) return false;
  const [given, expected] = await Promise.all([sha256(token), sha256(env.UPLOAD_TOKEN)]);
  return crypto.subtle.timingSafeEqual(given, expected);
}

function sha256(s: string): Promise<ArrayBuffer> {
  return crypto.subtle.digest("SHA-256", encoder.encode(s));
}
