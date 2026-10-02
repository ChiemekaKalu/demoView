import { useState } from 'preact/hooks'
import { Columns, Dumbbell, RateBars, SideKey, SplitBars } from '../findings/charts'
import data from '../findings/data.json'
import '../findings/findings.css'
import { count, interval, mapTitle, pct, type Rate, usd } from '../findings/format'
import { Link } from '../Link'
import { useTitle } from '../router'

const MODELS: Record<string, string> = {
  logistic: 'Trained logistic model',
  claude: 'Claude Opus 5.5',
  jev: 'Jev (Workers AI)',
  'gpt-oss': 'gpt-oss-120b',
}

const BUY_LABELS: Record<string, string> = { full: 'Full buy', force: 'Force', semi: 'Semi', eco: 'Eco' }

export function Findings() {
  useTitle('What decides a round')
  const { maps, post_plant: plant, opening, economy, clutches, predictability: pred, models } = data
  // From the rounded figures, so it agrees with the chart's labels.
  const spread = Math.round(maps[0].p * 100) - Math.round(maps[maps.length - 1].p * 100)
  return (
    <main class="page findings">
      <Link class="back" href="/">
        ← demoView
      </Link>
      <h1>What decides a CS2 round</h1>
      <p class="fx-lede">
        Win rates from {count(data.demos)} pro matches: {count(data.rounds)} regulation rounds across nine maps.
      </p>
      <p class="notice">
        These are pro GOTV demos. Tournament teams coordinate utility, manage money closely and rarely throw a round away,
        so the numbers may not transfer to matchmaking or Premier.
      </p>

      <dl class="fx-stats">
        <Stat value={pct(opening.opener_wins.p)} text="of rounds go to the side that gets the first kill" />
        <Stat value={pct(plant.by_alive_diff.find((d) => d.diff === 0)!.p)} text="T win rate after a plant with even numbers" />
        <Stat value={pct(economy.eco_beats_full.p)} text="of eco and semi-buy rounds beat a full buy" />
        <Stat value={`${spread} pts`} text="between the most CT-sided map and the most T-sided" />
      </dl>

      <section>
        <h2>Map balance</h2>
        <p>
          {mapTitle(maps[0].map)} is the most CT-sided map and {mapTitle(maps[maps.length - 1].map)} the most T-sided.
          Across all maps the CTs win {pct(data.ct_win.p)} of rounds, and pistol rounds are a coin flip (
          {pct(data.pistol_ct_win.p)} CT).
        </p>
        <figure>
          <SideKey />
          <SplitBars rows={maps.map((m) => ({ label: mapTitle(m.map), rate: m }))} unit="rounds" />
          <figcaption class="small muted">
            Share of regulation rounds won by each side. The whisker is the 95% interval of the CT share; Train and
            Vertigo have the fewest demos, so theirs are widest.
          </figcaption>
        </figure>
      </section>

      <section>
        <h2>After the plant, numbers decide it</h2>
        <p>
          The Ts win {pct(plant.t_win.p)} of the {count(plant.plants)} rounds where they plant. What matters most is the
          head count when the bomb goes down. Near even numbers, each player up or down moves the odds by 20 to 30
          points.
        </p>
        <figure>
          <Columns
            cols={plant.by_alive_diff.map((d) => ({ label: d.diff > 0 ? `+${d.diff}` : d.diff < 0 ? `−${-d.diff}` : '0', rate: d }))}
            side="t"
            unit="plants"
          />
          <figcaption class="small muted">
            T win rate by Ts alive minus CTs alive at the plant. The dashed line is 50%.
          </figcaption>
        </figure>
        <p>Late plants win a little less often, perhaps because the CTs have had time to rotate.</p>
        <RateBars rows={plant.by_time.map((d) => ({ key: d.when, label: d.when, rate: d, side: 't' }))} unit="plants" />
      </section>

      <section>
        <h2>The opening duel</h2>
        <p>
          The side that gets the first kill wins {pct(opening.opener_wins.p)} of rounds (CT {pct(opening.ct.p)}, T{' '}
          {pct(opening.t.p)}). A trade within 5 seconds lifts the side that lost the opener from about{' '}
          {pct(1 - opening.opener_wins.p)} to {pct(opening.traded.p)}.
        </p>
        <Spots spots={opening.spots} />
      </section>

      <section>
        <h2>Money</h2>
        <p>
          A side on an eco or semi-buy beats a full buy {pct(economy.eco_beats_full.p)} of the time, and a force buy beats
          it {pct(economy.force_beats_full.p)}. The pistol-round winner takes the next round{' '}
          {pct(economy.pistol_winner_wins_next.p)} of the time.
        </p>
        <BuyGrid buys={economy.buys} cells={economy.ct_win} />
      </section>

      <section>
        <h2>Clutches</h2>
        <p>
          Past a 1v1, a lone player rarely wins: {spanOf(clutches[1])} of 1v2s and {spanOf(clutches[2])} of 1v3s or
          worse. A lone T wins a 1v1 more often than a lone CT ({pct(clutches[0].t.p)} against {pct(clutches[0].ct.p)}),
          likely because many T clutches come after a plant, with the clock on their side.
        </p>
        <figure>
          <SideKey />
          <RateBars
            rows={clutches.flatMap((c) => [
              { key: `ct ${c.odds}`, label: `CT ${c.odds}`, rate: c.ct, side: 'ct' as const },
              { key: `t ${c.odds}`, label: `T ${c.odds}`, rate: c.t, side: 't' as const },
            ])}
            unit="clutches"
          />
          <figcaption class="small muted">Rounds won by the last player alive, by how many enemies were left.</figcaption>
        </figure>
      </section>

      <section>
        <h2>How predictable is a round?</h2>
        <p>
          A model that knows who's alive, the bomb and the clock scores a Brier of {pred.base.toFixed(4)} (0.25 is a
          coin flip, lower is better). Telling it the map barely helps ({pred.map.toFixed(4)}). Telling it what each side
          bought does, on every map ({pred.econ.toFixed(4)}).
        </p>
        <figure>
          <p class="fx-key small muted">
            <span class="fx-dot-key from" /> Alive, bomb and clock <span class="fx-dot-key to" /> Plus each side's buy
          </p>
          <Dumbbell
            rows={pred.maps.map((m) => ({ key: m.map, label: mapTitle(m.map), from: m.base, to: m.econ }))}
            domain={[0.12, 0.16]}
            ticks={[0.12, 0.13, 0.14, 0.15, 0.16]}
          />
          <figcaption class="small muted">
            Brier score by map at round start and after every kill, on demos the model wasn't trained on. Before the
            first kill, money alone scores between 0.20 and 0.23: better than a coin flip, but not by much.
          </figcaption>
        </figure>
      </section>

      <section>
        <h2>Match Pulse and the models</h2>
        <p>
          The line under each replay's scrubber is Match Pulse: the chance that the CTs win the round, after every
          kill. To choose what draws it, four models were asked about the same {models.moments} pro moments.
        </p>
        <div class="board">
          <table class="fx-models">
            <thead>
              <tr>
                <th>Model</th>
                <th>Brier</th>
                <th>
                  <abbr title="How well it ranks moments: 1 is perfect, 0.5 is guessing">AUC</abbr>
                </th>
                <th>Cost per call</th>
              </tr>
            </thead>
            <tbody>
              {models.rows.map((r) => (
                <tr key={r.model}>
                  <th scope="row">{MODELS[r.model] ?? r.model}</th>
                  <td>{r.brier.toFixed(3)}</td>
                  <td>{r.auc.toFixed(3)}</td>
                  <td>{usd(r.usd_per_call)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p>
          A small model trained on these demos scores best. Claude is the best-calibrated model with no training, and Jev
          ranks moments best at about 1/95 of Claude's cost. Pulse uses Jev, with the trained model as its fallback.
          gpt-oss runs on Workers AI and isn't priced per call here.
        </p>
        <p>
          A side result: each round was written up as a short, name-free story and embedded as text. With map names
          left out, a round's nearest neighbours from other matches were won the same way 98% of the time, against 30%
          by chance. The stories say how each round ended, so part of that is by construction.
        </p>
      </section>

      <section class="small muted">
        <h2>Method</h2>
        <p>
          {count(data.demos)} public pro GOTV demos were each read in the cloud, reduced to name-free per-round facts and
          deleted. Win rates carry 95% Wilson intervals: hover a bar to see one. Buys are
          each side's equipment value at the end of freeze time: eco under $5,000, semi under $10,000, force under
          $20,000. Predictability comes from a logistic model trained on four-fifths of the demos and scored on the
          rest. These are correlations, not causes.
        </p>
      </section>
    </main>
  )
}

function spanOf(c: { ct: Rate; t: Rate }): string {
  const [a, b] = [c.ct.p, c.t.p].sort((x, y) => x - y)
  return `${Math.round(a * 100)}–${pct(b)}`
}

function Stat({ value, text }: { value: string; text: string }) {
  return (
    <div>
      <dt>{value}</dt>
      <dd>{text}</dd>
    </div>
  )
}

interface Spot {
  place: string
  side: string
  share: number
  n: number
  p: number
  lo: number
  hi: number
}

function Spots({ spots }: { spots: Record<string, { openings: number; spots: Spot[] }> }) {
  const ids = Object.keys(spots)
  const [map, setMap] = useState('mirage')
  const cur = spots[map]
  const top = Math.max(...cur.spots.map((s) => s.share))
  return (
    <figure>
      <div class="fx-tabs" role="group" aria-label="Map">
        {ids.map((id) => (
          <button key={id} type="button" aria-pressed={id === map} onClick={() => setMap(id)}>
            {mapTitle(id)}
          </button>
        ))}
      </div>
      <div class="board">
        <table class="fx-spots">
          <caption class="sr">Most common opening duels on {mapTitle(map)}</caption>
          <thead>
            <tr>
              <th>Where the victim died</th>
              <th>Won by</th>
              <th>Share of openings</th>
              <th>Round won</th>
            </tr>
          </thead>
          <tbody>
            {cur.spots.map((s) => (
              <tr key={`${s.place} ${s.side}`} title={`${count(s.n)} duels; round won ${pct(s.p)} (${interval(s)})`}>
                <th scope="row">{s.place}</th>
                <td>
                  <span class={`fx-swatch ${s.side.toLowerCase()}`} /> {s.side}
                </td>
                <td>
                  <span class="fx-share">
                    <span class={`fx-fill ${s.side.toLowerCase()}`} style={{ width: `${(s.share / top) * 100}%` }} />
                  </span>
                  {pct(s.share)}
                </td>
                <td>{pct(s.p)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <figcaption class="small muted">
        The {cur.spots.length} most common opening kills on {mapTitle(map)}, out of {count(cur.openings)}. All maps
        together average {pct(data.opening.opener_wins.p)}.
      </figcaption>
    </figure>
  )
}

// CT win rate for each pair of buys. Cells lean CT blue above 50% and T
// orange below it; pairs with too few rounds are left blank.
function BuyGrid({ buys, cells }: { buys: string[]; cells: Record<string, Rate> }) {
  return (
    <figure>
      <div class="board">
        <table class="fx-grid">
          <caption class="sr">CT round win rate by each side's buy</caption>
          <thead>
            <tr>
              <th>CT buy ↓ · T buy →</th>
              {buys.map((t) => (
                <th key={t}>{BUY_LABELS[t]}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {buys.map((ct) => (
              <tr key={ct}>
                <th scope="row">{BUY_LABELS[ct]}</th>
                {buys.map((t) => {
                  const r = cells[`${ct} v ${t}`]
                  if (!r) return <td key={t} class="fx-cell empty" title="Fewer than 30 rounds" />
                  const lean = Math.round(Math.abs(r.p - 0.5) * 110)
                  return (
                    <td
                      key={t}
                      class={`fx-cell ${r.p >= 0.5 ? 'ct' : 't'}`}
                      style={{ '--lean': `${lean}%` }}
                      title={`CT ${BUY_LABELS[ct].toLowerCase()} against T ${BUY_LABELS[t].toLowerCase()}: CT win ${pct(r.p)} (${interval(r)}), ${count(r.n)} rounds`}
                    >
                      {pct(r.p)}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <figcaption class="small muted">CT win rate by each side's buy, pistol rounds excluded.</figcaption>
    </figure>
  )
}
