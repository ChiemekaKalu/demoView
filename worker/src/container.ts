import { Container, type StopParams } from "@cloudflare/containers";
import { errorText } from "./log";

// The parse service (parser/Dockerfile) running as a Cloudflare Container.
// It reads the demo from R2 and writes the parsed files back itself, so it
// gets the bucket's credentials and the shared parse secret at start, and the
// build it writes into each parse's manifest. Never log envVars.
export class ParserContainer extends Container<Env> {
  defaultPort = 8080;
  // Idle time before sleep is billed; parses take seconds.
  sleepAfter = "30s";

  envVars: Record<string, string> = {
    PARSE_SECRET: this.env.PARSE_SECRET,
    R2_ACCOUNT_ID: this.env.R2_ACCOUNT_ID,
    R2_BUCKET: this.env.R2_BUCKET,
    R2_ACCESS_KEY_ID: this.env.R2_ACCESS_KEY_ID,
    R2_SECRET_ACCESS_KEY: this.env.R2_SECRET_ACCESS_KEY,
    PARSER_BUILD: this.env.PARSER_BUILD,
  };

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
