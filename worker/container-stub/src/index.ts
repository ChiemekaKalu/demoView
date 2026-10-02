// Can a request to a container run for several minutes? Two ways to ask,
// both with Bearer STUB_TOKEN and ?seconds=N:
//
//   POST /stream  streams the container's response straight back, a line
//                 every 10 seconds, so a client can watch it arrive.
//   POST /start   queues one job; the consumer makes the same request (the
//                 real parse path: queue consumer -> Durable Object ->
//                 container) and stores the outcome for GET /result.
import { Container, getContainer } from "@cloudflare/containers";

interface Env {
  STUB_CONTAINER: DurableObjectNamespace<StubContainer>;
  STUB_QUEUE: Queue<{ seconds: number }>;
  STUB_TOKEN: string;
}

interface StubResult {
  state: "queued" | "done";
  seconds: number;
  ok?: boolean;
  status?: number;
  lines?: number;
  first_line_s?: number;
  elapsed_s?: number;
  error?: string;
}

const SERVER = `
import http.server, time, urllib.parse

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        query = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        seconds = int(query.get("seconds", ["360"])[0])
        self.send_response(200)
        self.send_header("content-type", "text/plain")
        self.end_headers()
        start = time.time()
        while time.time() - start < seconds:
            self.wfile.write(f"{int(time.time() - start)}s\\n".encode())
            self.wfile.flush()
            time.sleep(10)
        self.wfile.write(b"done\\n")

http.server.ThreadingHTTPServer(("", 8080), Handler).serve_forever()
`;

export class StubContainer extends Container<Env> {
  defaultPort = 8080;
  sleepAfter = "30s";
  entrypoint = ["python", "-u", "-c", SERVER];

  async saveResult(result: StubResult): Promise<void> {
    await this.ctx.storage.put("result", result);
  }

  async lastResult(): Promise<StubResult | null> {
    return (await this.ctx.storage.get<StubResult>("result")) ?? null;
  }
}

const container = (env: Env) => getContainer(env.STUB_CONTAINER, "stub");
const streamUrl = (seconds: number) => `http://container/stream?seconds=${seconds}`;

export default {
  async fetch(request, env) {
    if (!env.STUB_TOKEN || request.headers.get("authorization") !== `Bearer ${env.STUB_TOKEN}`) {
      return new Response("unauthorized", { status: 401 });
    }
    const url = new URL(request.url);
    const seconds = Number(url.searchParams.get("seconds") ?? 360);
    const route = `${request.method} ${url.pathname}`;
    if (route === "POST /stream") {
      return container(env).fetch(streamUrl(seconds));
    }
    if (route === "POST /start") {
      await container(env).saveResult({ state: "queued", seconds });
      await env.STUB_QUEUE.send({ seconds });
      return new Response(`queued a ${seconds}s request; poll GET /result\n`, { status: 202 });
    }
    if (route === "GET /result") {
      return Response.json(await container(env).lastResult());
    }
    return new Response("not found", { status: 404 });
  },

  async queue(batch, env) {
    for (const message of batch.messages) {
      const { seconds } = message.body;
      const started = Date.now();
      const elapsed = () => Math.round((Date.now() - started) / 1000);
      const result: StubResult = { state: "done", seconds, lines: 0 };
      try {
        const res = await container(env).fetch(streamUrl(seconds));
        result.status = res.status;
        const decoder = new TextDecoder();
        let last = "";
        for await (const chunk of res.body!) {
          for (const line of decoder.decode(chunk, { stream: true }).split("\n").filter(Boolean)) {
            result.first_line_s ??= elapsed();
            result.lines! += 1;
            last = line;
          }
        }
        result.ok = last === "done";
      } catch (err) {
        result.ok = false;
        result.error = String(err);
      }
      result.elapsed_s = elapsed();
      await container(env).saveResult(result);
      (result.ok ? console.log : console.error)(JSON.stringify({ event: "stub_result", ...result }));
      message.ack();
    }
  },
} satisfies ExportedHandler<Env, { seconds: number }>;
