import { getContainer } from "@cloudflare/containers";
import { handleRequest, type ParseJob } from "./api";
import { handleDeadLetters, handleParseBatch } from "./consumer";
import { runCron } from "./cron";
import { httpParser, withFault } from "./parser";

export { ParserContainer } from "./container";

export default {
  fetch: (request, env) => handleRequest(request, env),

  // The sweeper, reparse batch and cleanup, by cron (triggers in wrangler.jsonc).
  scheduled: (controller, env) => runCron(controller.cron, env, controller.scheduledTime),

  // One Worker consumes both queues. Dead letter queue names end in -dlq.
  async queue(batch, env) {
    if (batch.queue.endsWith("-dlq")) return handleDeadLetters(batch, env);
    // One container serves every parse; the consumer runs one at a time.
    const container = getContainer(env.PARSER);
    const parser = httpParser("http://parser", env.PARSE_SECRET, (input, init) => container.fetch(input, init));
    return handleParseBatch(batch, env, withFault(parser, env.FAULT));
  },
} satisfies ExportedHandler<Env, ParseJob>;
