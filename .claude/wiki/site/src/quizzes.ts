// Multiple-choice questions, keyed by wiki page slug. The first option is the
// right one; the quiz shuffles them. Each explanation points back to the page.

export interface Question {
  q: string
  options: [correct: string, ...wrong: string[]]
  why: string
}

export const quizzes: Record<string, Question[]> = {
  'big-picture': [
    {
      q: 'Why does the browser upload the demo straight to R2 instead of through the Worker?',
      options: ['A Worker is capped at 128 MB of memory and demos are far bigger', 'R2 uploads are free and Worker uploads are not', 'The Worker cannot read request bodies', 'So the upload token is never needed'],
      why: 'The Worker only hands out a presigned PUT URL. The demo never passes through it.',
    },
    {
      q: 'Which piece owns the database?',
      options: ['The Worker. The parser never touches D1', 'The parser, which writes rows as it goes', 'The viewer, through the API', 'Cloud Run, through a service account'],
      why: 'The parser returns match.json and the Worker consumer turns it into D1 rows.',
    },
    {
      q: 'Which file does a parse write last, and why?',
      options: ['manifest.json: its presence means the whole parse is in place', 'match.json, because it is the largest file', 'The first round file, because R2 allows one write per second', 'Nothing. All files are written at once'],
      why: 'The parser writes round files, then match.json, then the manifest, which acts as the commit marker for the whole parse.',
    },
  ],
  'demo-format': [
    {
      q: 'Which clock do we use for ticks?',
      options: ['The demo-frame tick', 'net_Tick from inside packets', 'Wall-clock time from the file header', 'Whichever is larger'],
      why: 'net_Tick runs on a different, offset clock.',
    },
    {
      q: 'How does the parser find a game event by name?',
      options: ['From the descriptor list in the demo, never a hardcoded id', 'By a table compiled from the first demo we saw', 'Event ids are stable, so it uses constants', 'By the order events appear in the file'],
      why: 'Event ids shift between patches, so each demo carries its own descriptor list.',
    },
    {
      q: 'An event says userid 0x01A3. Which player slot is that?',
      options: ['0xA3, the low byte', '0x01, the high byte', '0x01A3 as a whole', 'The slot is in the string table, not the id'],
      why: 'userid & 0xFF is the slot. The userinfo string table, keyed by slot, then gives the name and SteamID64.',
    },
    {
      q: 'What does bit 0x40 on a frame\'s command type mean?',
      options: ['The payload is snappy-compressed', 'The frame holds entity updates', 'The tick is relative to the last frame', 'The frame is the last one in the file'],
      why: 'Payloads are protobuf, optionally snappy-compressed.',
    },
  ],
  parser: [
    {
      q: 'Why do the CLI and the server share one parse.Run?',
      options: ['So both accept and reject exactly the same demos', 'To avoid compiling two binaries', 'Because the CLI cannot read files on its own', 'To share the object store code'],
      why: 'One parse path means a demo that fails locally fails in the cloud, and the other way round.',
    },
    {
      q: 'The parser exits with code 1 (bad_demo). What does the server turn that into?',
      options: ['HTTP 422, which the consumer treats as final', 'HTTP 500, which the queue retries', 'HTTP 200 with an error body', 'HTTP 404'],
      why: 'Exit 1 maps to 422 (do not retry). Exit 3, internal, maps to a 5xx.',
    },
    {
      q: 'On the oracle path, when does the server ship the oracle\'s match.json instead of ours?',
      options: ['When ours disagrees on players, rounds or kills', 'Whenever the oracle finishes first', 'When the demo is over 500 MB', 'On every first parse of a demo'],
      why: 'Round files index into kills, so the match.json and round files must always agree.',
    },
  ],
  'match-logic': [
    {
      q: 'Where does the live window start and end?',
      options: ['From the last begin_new_match to cs_win_panel_match', 'From the first spawn to the last death', 'From round_start 1 to round_end of the last round', 'From the demo header to the file info frame'],
      why: 'That drops warmup, the knife round and the server\'s post-match world deaths.',
    },
    {
      q: 'The last living T dies, and the bomb is planted. What happens?',
      options: ['Nothing is decided yet. The bomb still has to explode or be defused', 'CT win by elimination', 'T win by elimination', 'The round is a draw'],
      why: 'Elimination of T only counts while the bomb is not planted. Try the Retake scenario in the lab.',
    },
    {
      q: 'Who is team A?',
      options: ['The team on CT in round 1', 'The team with more kills', 'The team with the lower SteamID', 'The team that won round 1'],
      why: 'Teams are fixed for the whole match, and side_a records which side A played each round.',
    },
    {
      q: 'How is a hit counted toward ADR?',
      options: ['Victim HP before minus HP after (health)', 'dmg_health from the event', 'dmg_armor plus dmg_health', 'Always 100 on a kill'],
      why: 'dmg_health is uncapped and rounded. HP before minus after matches the game\'s m_iDamage exactly.',
    },
  ],
  schema: [
    {
      q: 'The server runs 64 ticks per second. At a round-file rate of 16 Hz, how many ticks pass between two frames?',
      options: ['4', '16', '64', '2'],
      why: 'tick_rate / hz = 64 / 16. Round files were first planned at 16 Hz. They are now every tick (64 Hz), where it is 1.',
    },
    {
      q: 'In a round file, what does hp 0 mean?',
      options: ['The player is dead, and x and y hold where they died', 'The player has not spawned yet', 'The player is disconnected', 'The player is invulnerable'],
      why: 'Players who never spawned that round are simply not listed.',
    },
    {
      q: 'Which frame does a kill belong to?',
      options: ['ceil((tick - start_tick) * hz / tick_rate)', 'floor of the same', 'tick / hz', 'The frame whose tick is nearest'],
      why: 'It is the first frame where the victim\'s hp is 0.',
    },
  ],
  worker: [
    {
      q: 'How many parse attempts does a job get?',
      options: ['3, which is max_retries 2 plus the first try', '2', '5', 'Unlimited until the DLQ fills'],
      why: 'MAX_ATTEMPTS in consumer.ts must match wrangler.jsonc.',
    },
    {
      q: 'The parser answers 422. What does the consumer do?',
      options: ['Marks the match failed and acks the message', 'Retries after 30 seconds', 'Sends it to the dead letter queue', 'Reparses with the oracle'],
      why: 'A bad demo will be bad on every attempt.',
    },
    {
      q: 'What happens to a message for a match that is already ready?',
      options: ['It is acked and skipped (job_skipped)', 'It is parsed again', 'It moves to the DLQ', 'It marks the match failed'],
      why: 'That makes redelivery safe.',
    },
  ],
  viewer: [
    {
      q: 'Which formula turns a game y into a radar pixel y?',
      options: ['py = (pos_y - y) / scale', 'py = (y - pos_y) / scale', 'py = y * scale + pos_y', 'py = y / scale'],
      why: 'Game y points up, image y points down.',
    },
    {
      q: 'Why does the replay interpolate yaw along the shorter arc?',
      options: ['So 359 degrees to 1 passes through 0 instead of spinning the long way', 'Because yaw is stored as radians', 'To save bandwidth', 'Because the radar is rotated'],
      why: 'A naive lerp from 359 to 1 swings through every angle in between.',
    },
    {
      q: 'What is playback time 0?',
      options: ['The end of freeze time', 'The round start', 'The first kill', 'The bomb plant'],
      why: 'Round files start at the round\'s freeze_end_tick.',
    },
  ],
  testing: [
    {
      q: 'Why is the oracle a separate Go module?',
      options: ['So parser/ cannot import it', 'To build faster', 'Because demoinfocs needs a different Go version', 'So it can be deployed on its own'],
      why: 'The parser is from scratch. Reference parsers are test oracles only.',
    },
    {
      q: 'What does the diff require of kills?',
      options: ['Tick, attacker, victim, weapon, round, headshot and assister all exact', 'Tick within 1', 'Only the count to match', 'Attacker and victim only'],
      why: 'Only ADR has a tolerance, and in practice it is exact too.',
    },
  ],
  infra: [
    {
      q: 'Why did the user pick Cloudflare Containers over Cloud Run?',
      options: ['$5 a month, one cloud, and no public endpoint or shared secret', 'Containers have a longer request limit', 'Cloud Run cannot run Go', 'Containers are faster to start'],
      why: 'The spec now says Containers throughout. The open question about long requests was answered by a 360 s test that ran in full.',
    },
    {
      q: 'What is the cost trap with Containers?',
      options: ['Idle time is billed, and the default sleepAfter is 10 minutes', 'Every request is billed twice', 'Outbound R2 traffic is charged', 'Container images are billed per pull'],
      why: 'Set sleepAfter to about 30 seconds, or stop the container when the parse returns.',
    },
  ],
}
