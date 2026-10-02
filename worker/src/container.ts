import { Container, type StopParams } from "@cloudflare/containers";
import { errorText } from "./log";

// The parse service (parser/Dockerfile) running as a Cloudflare Container.
// It reads the demo from R2 and writes the parsed files back itself, so it
// gets the bucket's credentials and the shared parse secret at start, the
// build it writes into each parse's manifest, and on dev the round mode. Never
// log envVars.
export class ParserContainer extends Container<Env> {
  defaultPort = 8080;
  // Idle time before sleep is billed; parses take seconds. The shadow compare
  // runs for a few seconds after each response, so dev, which runs it, waits
  // longer rather than stop the container mid-compare.
  sleepAfter = this.env.PARSER_SHADOW ? "2m" : "30s";

  envVars = containerEnv(this.env);

  override onStart(): void {
    console.log(JSON.stringify({ event: "container_start" }));
  }

  // An out-of-memory kill shows up here as exit code 137, by runtime signal.
  override onStop({ exitCode, reason }: StopParams): void {
    const log = exitCode === 0 ? console.log : console.error;
    log(JSON.stringify({ event: "container_stop", exit_code: exitCode, reason }));
  }

  override onError(err: unknown): never {
    console.error(JSON.stringify({ event: "container_error", error: errorText(err) }));
    throw err;
  }
}

// The container's environment, from the Worker's secrets and vars. The round
// mode only goes in when set: an empty PARSER_ROUNDS would override the
// image's PARSER_ROUNDS=oracle and ship no round files.
export function containerEnv(env: Env): Record<string, string> {
  return {
    PARSE_SECRET: env.PARSE_SECRET,
    R2_ACCOUNT_ID: env.R2_ACCOUNT_ID,
    R2_BUCKET: env.R2_BUCKET,
    R2_ACCESS_KEY_ID: env.R2_ACCESS_KEY_ID,
    R2_SECRET_ACCESS_KEY: env.R2_SECRET_ACCESS_KEY,
    PARSER_BUILD: env.PARSER_BUILD,
    ...(env.PARSER_ROUNDS ? { PARSER_ROUNDS: env.PARSER_ROUNDS } : {}),
    ...(env.PARSER_SHADOW ? { PARSER_SHADOW: env.PARSER_SHADOW } : {}),
  };
}
