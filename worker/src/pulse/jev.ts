// Jev (Workers AI `typesafe/jev`): one call takes a state and named, typed
// questions and returns typed answers. See .claude/ideas/jev/spec.md.

export interface Question {
  type: "noul" | "choice" | "score";
  instructions: string;
  // noul: { true, false } descriptions. choice: option -> description (or
  // null). score: one description per level, lowest first.
  criteria?: Record<string, string | null> | string[];
}

export type Answer =
  | { type: "noul"; noul: number }
  | { type: "choice"; choice: string; probabilities: Record<string, number>; confidence: number }
  | { type: "score"; score: number; probabilities: Record<string, number>; confidence: number };

export type Answers = Record<string, Answer>;

// Why a call failed for good. Out of credits is final for the whole match:
// no more calls, the fallback model draws the line.
export class JevError extends Error {
  readonly kind: "credits" | "rate_limit" | "other";

  constructor(message: string, kind: JevError["kind"]) {
    super(message);
    this.kind = kind;
  }
}

// What one match's calls cost and how they went, for the `pulse` log line.
export class Ledger {
  calls = 0;
  errors = 0;
  retries = 0;
  rateLimited = 0;
  inTokens = 0;
  private ms: number[] = [];

  record(ms: number, inTokens: number): void {
    this.calls++;
    this.inTokens += inTokens;
    this.ms.push(ms);
  }

  // $0.042 per 1M input tokens; output and cached input are free.
  get usd(): number {
    return (this.inTokens * 0.042) / 1e6;
  }

  percentile(p: number): number {
    if (this.ms.length === 0) return 0;
    const sorted = [...this.ms].sort((a, b) => a - b);
    return sorted[Math.min(sorted.length - 1, Math.floor(p * sorted.length))]!;
  }
}

// Spaces call starts at least gapMs apart with at most maxInFlight running,
// under the gateway's wholesale limit (429 code 2018 at about 9 requests a
// second; 4 a second ran clean over 3,805 calls).
export class Pacer {
  private active = 0;
  private nextStart = 0;
  private waiting: (() => void)[] = [];
  private maxInFlight: number;
  private gapMs: number;

  constructor(maxInFlight = 4, gapMs = 250) {
    this.maxInFlight = maxInFlight;
    this.gapMs = gapMs;
  }

  async run<T>(fn: () => Promise<T>): Promise<T> {
    while (this.active >= this.maxInFlight) await new Promise<void>((resolve) => this.waiting.push(resolve));
    this.active++;
    const now = Date.now();
    const start = Math.max(now, this.nextStart);
    this.nextStart = start + this.gapMs;
    if (start > now) await new Promise((resolve) => setTimeout(resolve, start - now));
    try {
      return await fn();
    } finally {
      this.active--;
      this.waiting.shift()?.();
    }
  }
}

const RETRIES = 4;

// The binding's run, typed loosely: typesafe/jev isn't in the Workers AI
// model list the types know.
type RunJev = (model: string, input: unknown, options: unknown) => Promise<unknown>;

export interface JevClient {
  ask(state: unknown, questions: Record<string, Question>): Promise<{ answers: Answers; model: string }>;
}

// Calls Jev through the AI binding and the account's AI Gateway, paced, with
// retries on rate limits. Out of credits fails at once and for every later
// call, so a match stops spending the moment the balance runs out.
export function jevClient(ai: Ai, gateway: string, ledger: Ledger, pacer = new Pacer()): JevClient {
  const run = (ai as unknown as { run: RunJev }).run.bind(ai);
  let outOfCredits = false;
  return {
    async ask(state, questions) {
      for (let attempt = 0; ; attempt++) {
        if (outOfCredits) throw new JevError("AI Gateway credits are empty", "credits");
        let started = Date.now();
        try {
          const out = await pacer.run(() => {
            started = Date.now(); // the call's own time, not its wait in the pacer
            return run("typesafe/jev", { state, questions }, { gateway: { id: gateway } });
          });
          const result = unwrap(out);
          ledger.record(Date.now() - started, result.usage?.input_tokens ?? 0);
          if (!result.answers || Object.keys(result.answers).length === 0) {
            throw new JevError("Jev returned no answers", "other");
          }
          return { answers: result.answers, model: result.model ?? "jev-unknown" };
        } catch (err) {
          const failure = err instanceof JevError ? err : classify(err);
          if (failure.kind === "credits") outOfCredits = true;
          if (failure.kind === "rate_limit") ledger.rateLimited++;
          if (failure.kind !== "rate_limit" || attempt >= RETRIES) {
            ledger.errors++;
            throw failure;
          }
          ledger.retries++;
          await new Promise((resolve) => setTimeout(resolve, 1000 + 2000 * attempt));
        }
      }
    },
  };
}

interface JevResult {
  answers?: Answers;
  model?: string;
  usage?: { input_tokens?: number };
}

// Workers AI wraps the model's own result once more.
function unwrap(out: unknown): JevResult {
  const o = (out ?? {}) as JevResult & { result?: JevResult };
  return o.answers ? o : (o.result ?? {});
}

// Gateway errors carry a numeric code: 2021 out of credits, 2018 the
// wholesale rate limit (with HTTP 429).
export function classify(err: unknown): JevError {
  const message = err instanceof Error ? err.message : String(err);
  if (/\b2021\b|insufficient balance/i.test(message)) return new JevError(message, "credits");
  if (/\b(2018|429)\b|rate limit/i.test(message)) return new JevError(message, "rate_limit");
  return new JevError(message, "other");
}
