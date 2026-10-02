// POST /start?seconds=N (Bearer STUB_TOKEN) queues one job. The consumer
// asks the container to stream a line every 10 seconds for N seconds and
// logs each line as it arrives. If the last log line says "done" after
// N seconds, a parse that long works through the real path:
// queue consumer -> Durable Object -> container.
import { Container, getContainer } from "@cloudflare/containers";

interface Env {
  STUB_CONTAINER: DurableObjectNamespace<StubContainer>;
  STUB_QUEUE: Queue<{ seconds: number }>;
  STUB_TOKEN: string;
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
  sleepAfter = "2m";
  entrypoint = ["python", "-u", "-c", SERVER];
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (request.method !== "POST" || url.pathname !== "/start") {
      return new Response("not found", { status: 404 });
    }
    if (!env.STUB_TOKEN || request.headers.get("authorization") !== `Bearer ${env.STUB_TOKEN}`) {
      return new Response("unauthorized", { status: 401 });
    }
    const seconds = Number(url.searchParams.get("seconds") ?? 360);
    await env.STUB_QUEUE.send({ seconds });
    return new Response(`queued a ${seconds}s request; watch \`wrangler tail\`\n`, { status: 202 });
  },

  async queue(batch, env) {
    for (const message of batch.messages) {
      const { seconds } = message.body;
      const started = Date.now();
      const elapsed = () => Math.round((Date.now() - started) / 1000);
      try {
        const res = await getContainer(env.STUB_CONTAINER, "stub").fetch(
          `http://container/stream?seconds=${seconds}`,
        );
        const decoder = new TextDecoder();
        let last = "";
        for await (const chunk of res.body!) {
          for (const line of decoder.decode(chunk, { stream: true }).split("\n").filter(Boolean)) {
            last = line;
            console.log(JSON.stringify({ event: "stub_progress", container: line, elapsed_s: elapsed() }));
          }
        }
        console.log(JSON.stringify({ event: "stub_result", ok: last === "done", status: res.status, seconds, elapsed_s: elapsed() }));
      } catch (err) {
        console.log(JSON.stringify({ event: "stub_result", ok: false, error: String(err), seconds, elapsed_s: elapsed() }));
      }
      message.ack();
    }
  },
} satisfies ExportedHandler<Env, { seconds: number }>;
