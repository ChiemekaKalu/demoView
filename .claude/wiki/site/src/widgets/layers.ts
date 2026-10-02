// Walks a demo file layer by layer, from the first bytes down to one game event.

import { h } from '../dom'

interface Layer {
  name: string
  where: string
  detail: string
  sample: string
}

const LAYERS: Layer[] = [
  {
    name: 'The file',
    where: 'the first 16 bytes',
    detail: 'A fixed preamble: the magic PBDEMS2 and a zero byte, then two little-endian int32 offsets, one to the file info and one to the spawn groups. The file info frame really does sit at the first offset, which makes a handy sanity check.',
    sample: '50 42 44 45 4D 53 32 00  |  file info offset  |  spawn groups offset',
  },
  {
    name: 'Frames',
    where: 'everything after the preamble',
    detail: 'Each frame is a varint command type, a varint tick and a varint size, then the payload. If bit 0x40 of the command is set, the payload is snappy-compressed. This tick is the clock we use everywhere. net_Tick, inside packets, runs offset from it.',
    sample: 'command DEM_Packet | 0x40   tick 18230   size 412   ->  snappy  ->  protobuf',
  },
  {
    name: 'Packets',
    where: 'inside DEM_Packet and DEM_SignonPacket',
    detail: 'A packet holds a bitstream, not another protobuf, and messages are not byte-aligned. That is why the project has its own bit reader.',
    sample: '1010 0111 0010 ...   (bits, packed)',
  },
  {
    name: 'Net messages',
    where: 'inside the bitstream',
    detail: 'Each one is a ubitvar type, a varint size, then a protobuf body. Type 205 is the list of event descriptors. Type 207 is a game event. svc_PacketEntities, which we have not decoded yet, carries positions.',
    sample: 'type 207   size 38   body CMsgSource1LegacyGameEvent',
  },
  {
    name: 'A game event',
    where: 'inside a type 207 message',
    detail: 'The event carries a descriptor id and values in key order. The id means nothing on its own: it is looked up by name in the list from message 205, because ids shift between patches. A player is a userid whose low byte is the slot, and the userinfo string table maps that slot to a name and SteamID64.',
    sample: 'player_death { userid 0x01A3 -> slot 0xA3, attacker 0x0107, weapon "ak47", headshot true }',
  },
]

export function mount(root: HTMLElement): void {
  let depth = 0
  const stack = h('ol', { class: 'peel' })
  const peel = h('button', { onclick: () => { depth = Math.min(depth + 1, LAYERS.length - 1); draw() } }, 'Next layer')
  const back = h('button', { class: 'ghost', onclick: () => { depth = 0; draw() } }, 'Reset')

  function draw() {
    stack.replaceChildren(...LAYERS.map((l, i) => {
      const open = i === depth
      return h('li', { class: `peel-row${open ? ' open' : ''}${i < depth ? ' gone' : ''}` },
        h('button', { class: 'peel-head', 'aria-expanded': String(open), onclick: () => { depth = i; draw() } },
          h('span', { class: 'peel-n' }, String(i + 1)),
          h('b', {}, l.name),
          h('span', { class: 'muted' }, l.where)),
        open ? h('div', { class: 'peel-body' }, h('p', {}, l.detail), h('pre', { class: 'mono readout' }, l.sample)) : null)
    }))
    peel.toggleAttribute('disabled', depth === LAYERS.length - 1)
  }
  root.append(h('div', { class: 'widget-row' }, peel, back), stack)
  draw()
}
