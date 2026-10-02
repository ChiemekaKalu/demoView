// Container time per month (UTC), so the reparse cron stays inside the
// allowance the Workers Paid plan includes.

// 25 GiB-hours of container memory a month: 25 hours of a basic instance,
// which has 1 GiB. Its vCPU allowance runs out at the same point.
export const CONTAINER_MS_PER_MONTH = 25 * 60 * 60 * 1000;

// After its last request the container idles this long before it sleeps
// (ParserContainer.sleepAfter), and idle time is billed too.
const IDLE_MS = 30_000;

function month(now: number): string {
  return new Date(now).toISOString().slice(0, 7);
}

// Counts one parse call. Each call counts the idle tail, which overcounts a
// run of back-to-back parses: the safe side for a cap.
export async function addContainerTime(db: D1Database, now: number, parseMs: number): Promise<void> {
  await db
    .prepare(
      `INSERT INTO container_usage (month, ms) VALUES (?1, ?2)
       ON CONFLICT (month) DO UPDATE SET ms = ms + ?2`,
    )
    .bind(month(now), parseMs + IDLE_MS)
    .run();
}

export async function containerTime(db: D1Database, now: number): Promise<number> {
  return (await db.prepare("SELECT ms FROM container_usage WHERE month = ?").bind(month(now)).first<number>("ms")) ?? 0;
}
