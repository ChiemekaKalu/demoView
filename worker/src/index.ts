import { handleRequest, type ParseJob } from "./api";
import { handleDeadLetters, handleParseBatch } from "./consumer";
import { cloudRunParser } from "./parser";

export default {
  fetch: (request, env) => handleRequest(request, env),

  // One Worker consumes both queues. Dead letter queue names end in -dlq.
  async queue(batch, env) {
    if (batch.queue.endsWith("-dlq")) return handleDeadLetters(batch, env);
    return handleParseBatch(batch, env, cloudRunParser(env.PARSER_URL, env.PARSE_SECRET));
  },
} satisfies ExportedHandler<Env, ParseJob>;
