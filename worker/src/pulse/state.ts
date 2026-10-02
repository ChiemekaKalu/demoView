// Jev states for a parsed match: compact, name-free descriptions of a round
// at one moment, or of the whole round with its result. A port of the
// experiments' "today" tier (.claude/ideas/jev): kills, alive counts, HP and
// the clock, from match.json and round files alone. Players appear only as
// p<idx>; names and Steam ids never enter a state.

// Goes up whenever the states or questions change, so answers from different
// prompts are never mixed. 2 will add the bomb timeline.
export const STATE_VERSION = 1;

// mp_roundtime_defuse, in seconds: the clock the states describe.
const ROUND_TIME = 115;

export type Side = "CT" | "T";

// The parts of match.json and a round file the states read.
export interface PulseMatch {
  match: { map: string; tick_rate: number };
  players: { idx: number; team: "A" | "B" }[];
  rounds: {
    n: number;
    start_tick: number;
    freeze_end_tick: number;
    end_tick: number;
    winner: "A" | "B";
    reason: string;
    side_a: Side;
  }[];
  kills: { round: number; tick: number; attacker: number | null; victim: number; weapon: string; headshot: boolean }[];
}

export interface PulseRound {
  start_tick: number;
  hz: number;
  players: { idx: number; hp: number[] }[];
}

export interface RoundEvent {
  type: "freeze_end" | "kill";
  tick: number;
  sec: number; // since freeze end
  afterEnd: boolean;
  alive: Record<Side, number>; // after this event
  kill?: { attacker: number | null; victim: number; aside?: Side; vside?: Side; weapon: string; headshot: boolean };
}

export class RoundView {
  readonly match: PulseMatch;
  readonly n: number;
  readonly file: PulseRound | null;
  readonly round: PulseMatch["rounds"][number];
  readonly side = new Map<number, Side>();
  readonly events: RoundEvent[] = [];

  constructor(match: PulseMatch, n: number, file: PulseRound | null) {
    this.match = match;
    this.n = n;
    this.file = file;
    const round = match.rounds.find((r) => r.n === n);
    if (!round) throw new Error(`match.json has no round ${n}`);
    this.round = round;
    // Everyone who spawned this round, else everyone in the match.
    const present = new Set(file?.players.map((p) => p.idx) ?? match.players.map((p) => p.idx));
    for (const p of match.players) if (present.has(p.idx)) this.side.set(p.idx, this.sideOf(p.team));

    const alive: Record<Side, number> = { CT: 0, T: 0 };
    for (const s of this.side.values()) alive[s]++;
    const fz = round.freeze_end_tick;
    const kills = match.kills.filter((k) => k.round === n);
    const ticks = [...kills.map((k) => k.tick), fz].sort((a, b) => a - b);
    let freezeShown = false;
    for (const tick of new Set(ticks)) {
      if (tick === fz && !freezeShown) {
        freezeShown = true;
        this.events.push({ type: "freeze_end", tick, sec: 0, afterEnd: false, alive: { ...alive } });
      }
      for (const k of kills.filter((k) => k.tick === tick)) {
        const afterEnd = k.tick > round.end_tick;
        const vside = this.side.get(k.victim);
        if (vside && alive[vside] > 0 && !afterEnd) alive[vside]--;
        const aside = k.attacker === null ? undefined : this.side.get(k.attacker);
        this.events.push({
          type: "kill",
          tick: k.tick,
          sec: (k.tick - fz) / match.match.tick_rate,
          afterEnd,
          alive: { ...alive },
          kill: { attacker: k.attacker, victim: k.victim, aside, vside, weapon: k.weapon, headshot: k.headshot },
        });
      }
    }
  }

  sideOf(team: "A" | "B"): Side {
    const a = this.round.side_a;
    return team === "A" ? a : a === "CT" ? "T" : "CT";
  }

  get winner(): Side {
    return this.sideOf(this.round.winner);
  }

  // The moments Jev reads: freeze end, then every kill before the round ended.
  get cuts(): number[] {
    return this.events.flatMap((e, i) => (e.type === "freeze_end" || !e.afterEnd ? [i] : []));
  }

  // The state at event index cut, or of the whole round with its result.
  state(opts: { cut?: number; outcome?: boolean; ids?: boolean } = {}): Record<string, unknown> {
    const { match, n, round } = this;
    const before = match.rounds.filter((r) => r.n < n);
    const a = before.filter((r) => r.winner === "A").length;
    const score = round.side_a === "CT" ? { CT: a, T: before.length - a } : { CT: before.length - a, T: a };
    const shown = opts.cut === undefined ? this.events : this.events.slice(0, opts.cut + 1);
    let timeline = shown.flatMap((e) => (e.kill ? [describeKill(e)] : []));
    if (opts.ids) timeline = timeline.map((line, i) => `L${String(i).padStart(2, "0")} ${line}`);

    const state: Record<string, unknown> = {
      map: match.match.map,
      round: opts.outcome ? `${n} of ${match.rounds.length}` : String(n),
      score_before_round: `CT ${score.CT} - T ${score.T}`,
      players: { CT: this.ids("CT"), T: this.ids("T") },
      timeline: timeline.length ? timeline : ["Freeze time ends."],
    };
    if (opts.cut !== undefined) {
      const now = shown.at(-1)!;
      state.now = {
        time: `about ${Math.max(0, roundHalfEven(ROUND_TIME - now.sec))} seconds left in the round`,
        alive: `${now.alive.CT} CT v ${now.alive.T} T`,
        alive_players: { CT: this.alivePlayers("CT", now.tick), T: this.alivePlayers("T", now.tick) },
      };
    }
    if (opts.outcome) state.result = `${this.winner} win by ${round.reason.replaceAll("_", " ")}`;
    return state;
  }

  private ids(side: Side): string[] {
    return [...this.side].filter(([, s]) => s === side).sort(([a], [b]) => a - b).map(([i]) => `p${i}`);
  }

  // Players still alive at tick, with their HP as a word.
  private alivePlayers(side: Side, tick: number): string[] {
    const dead = new Set(this.events.filter((e) => e.kill && e.tick <= tick).map((e) => e.kill!.victim));
    return [...this.side]
      .filter(([i, s]) => s === side && !dead.has(i))
      .sort(([a], [b]) => a - b)
      .map(([i]) => {
        const hp = this.hpAt(i, tick);
        return hp === null ? `p${i}` : `p${i} ${hpWord(hp)}`;
      });
  }

  private hpAt(idx: number, tick: number): number | null {
    const track = this.file?.players.find((p) => p.idx === idx);
    if (!this.file || !track || track.hp.length === 0) return null;
    const f = Math.ceil(((tick - this.file.start_tick) * this.file.hz) / this.match.match.tick_rate);
    return track.hp[Math.min(Math.max(f, 0), track.hp.length - 1)]!;
  }
}

function describeKill(e: RoundEvent): string {
  const k = e.kill!;
  const attacker = k.attacker === null ? "the world" : `p${k.attacker} (${k.aside ?? "?"})`;
  const verb = k.attacker !== null && k.aside === k.vside ? "team-kills" : "kills";
  const bits = [k.weapon || "?", ...(k.headshot ? ["headshot"] : [])];
  const tail = e.afterEnd ? " (after the round was decided)" : "";
  return `${clock(e.sec)} ${attacker} ${verb} p${k.victim} (${k.vside ?? "?"}) (${bits.join(", ")}). Alive: ${e.alive.CT} CT v ${e.alive.T} T.${tail}`;
}

function clock(sec: number): string {
  const s = Math.max(0, Math.trunc(sec));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

// Words, not numbers: Jev reads HP buckets better than HP.
export function hpWord(hp: number): string {
  if (hp >= 90) return "full HP";
  if (hp >= 50) return "hurt";
  if (hp >= 20) return "low HP";
  return "one shot from death";
}

// Python's round(), which the states were calibrated with: halves go to even.
function roundHalfEven(x: number): number {
  const r = Math.round(x);
  return Math.abs(x % 1) === 0.5 && r % 2 !== 0 ? r - 1 : r;
}
