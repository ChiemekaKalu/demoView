import { AwsClient } from "aws4fetch";

export const UPLOAD_URL_TTL_SECONDS = 3600;

// Presigns a PUT to R2's S3 endpoint so the browser uploads the demo
// directly. Signing is local HMAC work, so it needs no network and works with
// any credentials; R2 only checks them when the upload arrives.
export async function presignUpload(env: Env, key: string): Promise<string> {
  const client = new AwsClient({
    accessKeyId: env.R2_ACCESS_KEY_ID,
    secretAccessKey: env.R2_SECRET_ACCESS_KEY,
    service: "s3",
    region: "auto",
  });
  const url = new URL(
    `https://${env.R2_ACCOUNT_ID}.r2.cloudflarestorage.com/${env.R2_BUCKET_NAME}/${key}`,
  );
  url.searchParams.set("X-Amz-Expires", String(UPLOAD_URL_TTL_SECONDS));
  const signed = await client.sign(new Request(url, { method: "PUT" }), {
    aws: { signQuery: true },
  });
  return signed.url;
}
