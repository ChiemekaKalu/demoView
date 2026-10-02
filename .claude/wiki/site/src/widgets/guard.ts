// The server's consistency guard: ours ships only when it agrees with the
// oracle, so round files and match.json always describe the same match.

import { h } from '../dom'

export function mount(root: HTMLElement): void {
  let agrees = true
  const agree = h('button', { onclick: () => { agrees = true; draw() } }, 'Ours agrees')
  const differ = h('button', { class: 'ghost', onclick: () => { agrees = false; draw() } }, 'Ours differs on a kill')
  const board = h('div', { class: 'guard' })

  function draw() {
    agree.className = agrees ? '' : 'ghost'
    differ.className = agrees ? 'ghost' : ''
    const ship = agrees ? 'ours' : 'oracle'
    board.replaceChildren(
      h('div', { class: 'guard-row' },
        h('div', { class: 'guard-box' }, h('b', {}, 'parse.Run'), h('span', { class: 'muted' }, 'our match.json: players, rounds, kills')),
        h('div', { class: `guard-check ${agrees ? 'ok' : 'bad'}` }, agrees ? 'same players, rounds, kills' : 'kill 41 differs'),
        h('div', { class: 'guard-box' }, h('b', {}, 'oracle export'), h('span', { class: 'muted' }, 'round files, plus its own match.json')),
      ),
      h('ol', { class: 'guard-steps' },
        h('li', {}, 'Upload the oracle\'s ', h('code', {}, 'rounds/NN.json'), ' files.'),
        h('li', {}, 'Upload ', h('b', {}, ship === 'ours' ? 'our' : 'the oracle\'s'), ' ', h('code', {}, 'match.json'), ' last.'),
        h('li', {}, 'Log ', h('code', {}, `match_source: ${ship}`), agrees ? '.' : ', and a warning.')),
      h('p', { class: 'muted' }, agrees
        ? 'Round files refer to kills by index. Because both sides list the same kills, each index resolves to the correct kill.'
        : 'If our kill list differed, an index in a round file could point at the wrong kill. So the oracle\'s match.json ships instead, and the pair always agrees.'),
    )
  }
  root.append(h('div', { class: 'widget-row' }, agree, differ), board)
  draw()
}
