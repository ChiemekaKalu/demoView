package entities

import (
	"fmt"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
)

// maxPathDepth is the most indices a field path holds.
const maxPathDepth = 7

// A fieldPath locates one value in an entity. Index 0 picks a field of the
// class's serializer, and each later index steps into the nested
// serializer or array of the field before it.
type fieldPath struct {
	idx  [maxPathDepth]int32
	last int // index of the last element of idx in use
}

// fieldPathOp is an operation in the field path stream. Each changes the
// current path, and every op but opFinish then emits it. In the comments,
// p is the path, p[last] its last index, "push v" appends v, "pop n" drops
// the last n indices, "fp" reads a ReadUBitVarFP, "ubv" a ReadUBitVar, "sv"
// a zigzag varint and "N bits" a plain N-bit read. nonTopo(x) reads a bit
// for each index of p, and adds x to the index if it is set.
type fieldPathOp uint8

const (
	opPlusOne   fieldPathOp = iota // p[last] += 1
	opPlusTwo                      // p[last] += 2
	opPlusThree                    // p[last] += 3
	opPlusFour                     // p[last] += 4
	opPlusN                        // p[last] += fp + 5

	opPushOneLeftDeltaZeroRightZero          // push 0
	opPushOneLeftDeltaZeroRightNonZero       // push fp
	opPushOneLeftDeltaOneRightZero           // p[last] += 1, push 0
	opPushOneLeftDeltaOneRightNonZero        // p[last] += 1, push fp
	opPushOneLeftDeltaNRightZero             // p[last] += fp, push 0
	opPushOneLeftDeltaNRightNonZero          // p[last] += fp + 2, push fp + 1
	opPushOneLeftDeltaNRightNonZeroPack6Bits // p[last] += 3 bits + 2, push 3 bits + 1
	opPushOneLeftDeltaNRightNonZeroPack8Bits // p[last] += 4 bits + 2, push 4 bits + 1

	opPushTwoLeftDeltaZero        // push fp twice
	opPushTwoPack5LeftDeltaZero   // push 5 bits twice
	opPushThreeLeftDeltaZero      // push fp three times
	opPushThreePack5LeftDeltaZero // push 5 bits three times
	opPushTwoLeftDeltaOne         // p[last] += 1, push fp twice
	opPushTwoPack5LeftDeltaOne    // p[last] += 1, push 5 bits twice
	opPushThreeLeftDeltaOne       // p[last] += 1, push fp three times
	opPushThreePack5LeftDeltaOne  // p[last] += 1, push 5 bits three times
	opPushTwoLeftDeltaN           // p[last] += ubv + 2, push fp twice
	opPushTwoPack5LeftDeltaN      // p[last] += ubv + 2, push 5 bits twice
	opPushThreeLeftDeltaN         // p[last] += ubv + 2, push fp three times
	opPushThreePack5LeftDeltaN    // p[last] += ubv + 2, push 5 bits three times
	opPushN                       // n = ubv, p[last] += ubv, push fp n times
	opPushNAndNonTopological      // nonTopo(sv + 1), then push fp ubv times

	opPopOnePlusOne              // pop 1, p[last] += 1
	opPopOnePlusN                // pop 1, p[last] += fp + 1
	opPopAllButOnePlusOne        // pop to 1 index, p[0] += 1
	opPopAllButOnePlusN          // pop to 1 index, p[0] += fp + 1
	opPopAllButOnePlusNPack3Bits // pop to 1 index, p[0] += 3 bits + 1
	opPopAllButOnePlusNPack6Bits // pop to 1 index, p[0] += 6 bits + 1
	opPopNPlusOne                // pop fp, p[last] += 1
	opPopNPlusN                  // pop fp, p[last] += sv
	opPopNAndNonTopological      // pop fp, nonTopo(sv)

	opNonTopoComplex            // nonTopo(sv)
	opNonTopoPenultimatePlusOne // p[last-1] += 1
	opNonTopoComplexPack4Bits   // nonTopo(4 bits - 7)
	opFinish                    // end of the list

	numFieldPathOps
)

// fieldPathWeights are the ops' relative frequencies, from which their
// Huffman codes are built. The codes need every op, so a weight of 0
// counts as 1.
var fieldPathWeights = [numFieldPathOps]int{
	opPlusOne:                                36271,
	opPlusTwo:                                10334,
	opPlusThree:                              1375,
	opPlusFour:                               646,
	opPlusN:                                  4128,
	opPushOneLeftDeltaZeroRightZero:          35,
	opPushOneLeftDeltaZeroRightNonZero:       3,
	opPushOneLeftDeltaOneRightZero:           521,
	opPushOneLeftDeltaOneRightNonZero:        2942,
	opPushOneLeftDeltaNRightZero:             560,
	opPushOneLeftDeltaNRightNonZero:          471,
	opPushOneLeftDeltaNRightNonZeroPack6Bits: 10530,
	opPushOneLeftDeltaNRightNonZeroPack8Bits: 251,
	opPushNAndNonTopological:                 310,
	opPopOnePlusOne:                          2,
	opPopAllButOnePlusOne:                    1837,
	opPopAllButOnePlusN:                      149,
	opPopAllButOnePlusNPack3Bits:             300,
	opPopAllButOnePlusNPack6Bits:             634,
	opPopNAndNonTopological:                  1,
	opNonTopoComplex:                         76,
	opNonTopoPenultimatePlusOne:              271,
	opNonTopoComplexPack4Bits:                99,
	opFinish:                                 25474,
}

// fieldPathTree is the Huffman tree of the ops, as child pairs indexed by
// the next bit. A child c >= 0 is another node; c < 0 is the leaf for op ^c.
// Node 0 is the root.
var fieldPathTree = buildFieldPathTree()

// buildFieldPathTree builds the tree by repeatedly joining the two lightest
// nodes, the lighter of the pair as the 0 child. Among equal weights, the
// node made last is the lighter, with the ops made first, in order.
func buildFieldPathTree() [][2]int16 {
	type node struct {
		weight int
		order  int   // creation order
		ref    int16 // as a child in the tree
	}
	pending := make([]node, numFieldPathOps)
	for op, w := range fieldPathWeights {
		pending[op] = node{max(w, 1), op, ^int16(op)}
	}
	lightest := func() node {
		best := 0
		for i, n := range pending {
			b := pending[best]
			if n.weight < b.weight || n.weight == b.weight && n.order > b.order {
				best = i
			}
		}
		n := pending[best]
		pending = append(pending[:best], pending[best+1:]...)
		return n
	}
	var joined [][2]int16 // in creation order, so the root is last
	for len(pending) > 1 {
		a, b := lightest(), lightest()
		joined = append(joined, [2]int16{a.ref, b.ref})
		pending = append(pending, node{a.weight + b.weight, int(numFieldPathOps) + len(joined), int16(len(joined) - 1)})
	}
	n := int16(len(joined))
	tree := make([][2]int16, n)
	for i, kids := range joined {
		for j, c := range kids {
			if c >= 0 {
				kids[j] = n - 1 - c
			}
		}
		tree[n-1-int16(i)] = kids
	}
	return tree
}

// readFieldPaths reads a field path list up to opFinish and appends each
// path to paths.
func readFieldPaths(r *bitread.Reader, paths []fieldPath) ([]fieldPath, error) {
	p := fieldPath{idx: [maxPathDepth]int32{-1}}
	for {
		var n int16
		for {
			b := 0
			if r.ReadBit() {
				b = 1
			}
			if n = fieldPathTree[n][b]; n < 0 {
				break
			}
		}
		op := fieldPathOp(^n)
		if op == opFinish {
			return paths, r.Err()
		}
		if err := p.apply(op, r); err != nil {
			return paths, err
		}
		if err := r.Err(); err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
}

// apply changes p by op, reading the op's operands from r.
func (p *fieldPath) apply(op fieldPathOp, r *bitread.Reader) error {
	switch op {
	case opPlusOne:
		p.idx[p.last]++
	case opPlusTwo:
		p.idx[p.last] += 2
	case opPlusThree:
		p.idx[p.last] += 3
	case opPlusFour:
		p.idx[p.last] += 4
	case opPlusN:
		p.idx[p.last] += int32(r.ReadUBitVarFP()) + 5

	case opPushOneLeftDeltaZeroRightZero:
		return p.push(0)
	case opPushOneLeftDeltaZeroRightNonZero:
		return p.push(int32(r.ReadUBitVarFP()))
	case opPushOneLeftDeltaOneRightZero:
		p.idx[p.last]++
		return p.push(0)
	case opPushOneLeftDeltaOneRightNonZero:
		p.idx[p.last]++
		return p.push(int32(r.ReadUBitVarFP()))
	case opPushOneLeftDeltaNRightZero:
		p.idx[p.last] += int32(r.ReadUBitVarFP())
		return p.push(0)
	case opPushOneLeftDeltaNRightNonZero:
		p.idx[p.last] += int32(r.ReadUBitVarFP()) + 2
		return p.push(int32(r.ReadUBitVarFP()) + 1)
	case opPushOneLeftDeltaNRightNonZeroPack6Bits:
		p.idx[p.last] += int32(r.ReadBits(3)) + 2
		return p.push(int32(r.ReadBits(3)) + 1)
	case opPushOneLeftDeltaNRightNonZeroPack8Bits:
		p.idx[p.last] += int32(r.ReadBits(4)) + 2
		return p.push(int32(r.ReadBits(4)) + 1)

	case opPushTwoLeftDeltaZero:
		return p.pushFP(r, 2)
	case opPushTwoPack5LeftDeltaZero:
		return p.push5(r, 2)
	case opPushThreeLeftDeltaZero:
		return p.pushFP(r, 3)
	case opPushThreePack5LeftDeltaZero:
		return p.push5(r, 3)
	case opPushTwoLeftDeltaOne:
		p.idx[p.last]++
		return p.pushFP(r, 2)
	case opPushTwoPack5LeftDeltaOne:
		p.idx[p.last]++
		return p.push5(r, 2)
	case opPushThreeLeftDeltaOne:
		p.idx[p.last]++
		return p.pushFP(r, 3)
	case opPushThreePack5LeftDeltaOne:
		p.idx[p.last]++
		return p.push5(r, 3)
	case opPushTwoLeftDeltaN:
		p.idx[p.last] += int32(r.ReadUBitVar()) + 2
		return p.pushFP(r, 2)
	case opPushTwoPack5LeftDeltaN:
		p.idx[p.last] += int32(r.ReadUBitVar()) + 2
		return p.push5(r, 2)
	case opPushThreeLeftDeltaN:
		p.idx[p.last] += int32(r.ReadUBitVar()) + 2
		return p.pushFP(r, 3)
	case opPushThreePack5LeftDeltaN:
		p.idx[p.last] += int32(r.ReadUBitVar()) + 2
		return p.push5(r, 3)
	case opPushN:
		n := int(r.ReadUBitVar())
		p.idx[p.last] += int32(r.ReadUBitVar())
		return p.pushFP(r, n)
	case opPushNAndNonTopological:
		p.nonTopo(r, func() int32 { return r.ReadVarInt32() + 1 })
		return p.pushFP(r, int(r.ReadUBitVar()))

	case opPopOnePlusOne:
		if err := p.pop(1); err != nil {
			return err
		}
		p.idx[p.last]++
	case opPopOnePlusN:
		if err := p.pop(1); err != nil {
			return err
		}
		p.idx[p.last] += int32(r.ReadUBitVarFP()) + 1
	case opPopAllButOnePlusOne:
		p.last = 0
		p.idx[0]++
	case opPopAllButOnePlusN:
		p.last = 0
		p.idx[0] += int32(r.ReadUBitVarFP()) + 1
	case opPopAllButOnePlusNPack3Bits:
		p.last = 0
		p.idx[0] += int32(r.ReadBits(3)) + 1
	case opPopAllButOnePlusNPack6Bits:
		p.last = 0
		p.idx[0] += int32(r.ReadBits(6)) + 1
	case opPopNPlusOne:
		if err := p.pop(int(r.ReadUBitVarFP())); err != nil {
			return err
		}
		p.idx[p.last]++
	case opPopNPlusN:
		if err := p.pop(int(r.ReadUBitVarFP())); err != nil {
			return err
		}
		p.idx[p.last] += r.ReadVarInt32()
	case opPopNAndNonTopological:
		if err := p.pop(int(r.ReadUBitVarFP())); err != nil {
			return err
		}
		p.nonTopo(r, r.ReadVarInt32)

	case opNonTopoComplex:
		p.nonTopo(r, r.ReadVarInt32)
	case opNonTopoPenultimatePlusOne:
		if p.last == 0 {
			return fmt.Errorf("field path: no penultimate index to add to")
		}
		p.idx[p.last-1]++
	case opNonTopoComplexPack4Bits:
		p.nonTopo(r, func() int32 { return int32(r.ReadBits(4)) - 7 })
	}
	return nil
}

// nonTopo adds delta() to each index whose flag bit is set.
func (p *fieldPath) nonTopo(r *bitread.Reader, delta func() int32) {
	for i := 0; i <= p.last; i++ {
		if r.ReadBit() {
			p.idx[i] += delta()
		}
	}
}

func (p *fieldPath) push(v int32) error {
	if p.last == maxPathDepth-1 {
		return fmt.Errorf("field path: deeper than %d", maxPathDepth)
	}
	p.last++
	p.idx[p.last] = v
	return nil
}

func (p *fieldPath) pushFP(r *bitread.Reader, n int) error {
	for range n {
		if err := p.push(int32(r.ReadUBitVarFP())); err != nil {
			return err
		}
	}
	return nil
}

func (p *fieldPath) push5(r *bitread.Reader, n int) error {
	for range n {
		if err := p.push(int32(r.ReadBits(5))); err != nil {
			return err
		}
	}
	return nil
}

func (p *fieldPath) pop(n int) error {
	if n > p.last {
		return fmt.Errorf("field path: pop %d of %d indices", n, p.last+1)
	}
	p.last -= n
	return nil
}
