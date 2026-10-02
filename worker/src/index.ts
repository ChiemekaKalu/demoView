import { getContainer } from "@cloudflare/containers";
import { handleRequest, type ParseJob } from "./api";
import { handleDeadLetters, handleParseBatch } from "./consumer";
import { runCron } from "./cron";
import { httpParser, withFault } from "./parser";
import { handlePulseBatch, type PulseJob } from "./pulse/consumer";

export { ParserContainer } from "./container";

export default {
  fetch: (request, env) => handleRequest(request, env),

  // The sweeper, reparse batch and cleanup, by cron (triggers in wrangler.jsonc).
  scheduled: (controller, env) => runCron(controller.cron, env, controller.scheduledTime),

  // One Worker consumes the parse and pulse queues and their dead letter
  // queues, whose names end in -dlq.
  async queue(batch, env) {
    if (batch.queue.endsWith("-pulse-dlq")) {
      for (const m of batch.messages) {
        console.error(JSON.stringify({ event: "pulse_dead_letter", job: m.body, attempts: m.attempts }));
        m.ack();
      }
      return;
    }
    if (batch.queue.endsWith("-pulse")) return handlePulseBatch(batch as MessageBatch<PulseJob>, env);
    if (batch.queue.endsWith("-dlq")) return handleDeadLetters(batch as MessageBatch<ParseJob>, env);
    // One container serves every parse; the consumer runs one at a time.
    const container = getContainer(env.PARSER);
    const parser = httpParser("http://parser", env.PARSE_SECRET, (input, init) => container.fetch(input, init));
    return handleParseBatch(batch as MessageBatch<ParseJob>, env, withFault(parser, env.FAULT));
  },
} satisfies ExportedHandler<Env, ParseJob | PulseJob>;
