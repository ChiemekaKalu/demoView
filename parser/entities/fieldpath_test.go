package entities

import (
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
)

func TestFieldPathCodes(t *testing.T) {
	codes := fieldPathCodes()
	seen := map[string]fieldPathOp{}
	for op, c := range codes {
		if len(c) == 0 {
			t.Fatalf("op %d has no code", op)
		}
		s := bitString(c)
		if prev, ok := seen[s]; ok {
			t.Fatalf("ops %d and %d share code %s", prev, op, s)
		}
		seen[s] = fieldPathOp(op)
	}
	// The codes that the fixture demos line up with, for the common ops.
	for op, want := range map[fieldPathOp]string{
		opPlusOne:                                "0",
		opFinish:                                 "10",
		opPlusTwo:                                "1110",
		opPushOneLeftDeltaNRightNonZeroPack6Bits: "1111",
		opPushOneLeftDeltaOneRightNonZero:        "11000",
		opPlusN:                                  "11010",
		opPopAllButOnePlusOne:                    "110011",
	} {
		if got := bitString(codes[op]); got != want {
			t.Errorf("op %d: code %s, want %s", op, got, want)
		}
	}
}

func bitString(bits []bool) string {
	var b strings.Builder
	for _, x := range bits {
		b.WriteByte("01"[btoi(x)])
	}
	return b.String()
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestFieldPathOps(t *testing.T) {
	fp := func(vs ...uint32) func(w *BitWriter) {
		return func(w *BitWriter) {
			for _, v := range vs {
				w.UBitVarFP(v)
			}
		}
	}
	bits := func(n int, vs ...uint64) func(w *BitWriter) {
		return func(w *BitWriter) {
			for _, v := range vs {
				w.Bits(v, n)
			}
		}
	}
	ubvThen := func(v uint32, rest func(w *BitWriter)) func(w *BitWriter) {
		return func(w *BitWriter) { w.UBitVar(v); rest(w) }
	}
	tests := []struct {
		op       fieldPathOp
		from     []int32
		operands func(w *BitWriter)
		want     []int32
	}{
		{opPlusOne, []int32{3}, nil, []int32{4}},
		{opPlusTwo, []int32{3}, nil, []int32{5}},
		{opPlusThree, []int32{3}, nil, []int32{6}},
		{opPlusFour, []int32{3}, nil, []int32{7}},
		{opPlusN, []int32{3}, fp(2), []int32{10}},
		{opPushOneLeftDeltaZeroRightZero, []int32{3}, nil, []int32{3, 0}},
		{opPushOneLeftDeltaZeroRightNonZero, []int32{3}, fp(6), []int32{3, 6}},
		{opPushOneLeftDeltaOneRightZero, []int32{3}, nil, []int32{4, 0}},
		{opPushOneLeftDeltaOneRightNonZero, []int32{3}, fp(6), []int32{4, 6}},
		{opPushOneLeftDeltaNRightZero, []int32{3}, fp(5), []int32{8, 0}},
		{opPushOneLeftDeltaNRightNonZero, []int32{3}, fp(5, 6), []int32{10, 7}},
		{opPushOneLeftDeltaNRightNonZeroPack6Bits, []int32{3}, bits(3, 5, 6), []int32{10, 7}},
		{opPushOneLeftDeltaNRightNonZeroPack8Bits, []int32{3}, bits(4, 9, 10), []int32{14, 11}},
		{opPushTwoLeftDeltaZero, []int32{3}, fp(1, 2), []int32{3, 1, 2}},
		{opPushTwoPack5LeftDeltaZero, []int32{3}, bits(5, 1, 2), []int32{3, 1, 2}},
		{opPushThreeLeftDeltaZero, []int32{3}, fp(1, 2, 3), []int32{3, 1, 2, 3}},
		{opPushThreePack5LeftDeltaZero, []int32{3}, bits(5, 1, 2, 3), []int32{3, 1, 2, 3}},
		{opPushTwoLeftDeltaOne, []int32{3}, fp(1, 2), []int32{4, 1, 2}},
		{opPushTwoPack5LeftDeltaOne, []int32{3}, bits(5, 1, 2), []int32{4, 1, 2}},
		{opPushThreeLeftDeltaOne, []int32{3}, fp(1, 2, 3), []int32{4, 1, 2, 3}},
		{opPushThreePack5LeftDeltaOne, []int32{3}, bits(5, 1, 2, 3), []int32{4, 1, 2, 3}},
		{opPushTwoLeftDeltaN, []int32{3}, ubvThen(4, fp(1, 2)), []int32{9, 1, 2}},
		{opPushTwoPack5LeftDeltaN, []int32{3}, ubvThen(4, bits(5, 1, 2)), []int32{9, 1, 2}},
		{opPushThreeLeftDeltaN, []int32{3}, ubvThen(4, fp(1, 2, 3)), []int32{9, 1, 2, 3}},
		{opPushThreePack5LeftDeltaN, []int32{3}, ubvThen(4, bits(5, 1, 2, 3)), []int32{9, 1, 2, 3}},
		{opPushN, []int32{3}, ubvThen(2, ubvThen(4, fp(1, 2))), []int32{7, 1, 2}},
		{opPushNAndNonTopological, []int32{3, 5}, func(w *BitWriter) {
			w.Bit(true)
			w.VarInt(1) // +1 more
			w.Bit(false)
			w.UBitVar(1)
			w.UBitVarFP(9)
		}, []int32{5, 5, 9}},
		{opPopOnePlusOne, []int32{3, 5}, nil, []int32{4}},
		{opPopOnePlusN, []int32{3, 5}, fp(2), []int32{6}},
		{opPopAllButOnePlusOne, []int32{3, 5, 7}, nil, []int32{4}},
		{opPopAllButOnePlusN, []int32{3, 5, 7}, fp(2), []int32{6}},
		{opPopAllButOnePlusNPack3Bits, []int32{3, 5, 7}, bits(3, 2), []int32{6}},
		{opPopAllButOnePlusNPack6Bits, []int32{3, 5, 7}, bits(6, 40), []int32{44}},
		{opPopNPlusOne, []int32{3, 5, 7}, fp(2), []int32{4}},
		{opPopNPlusN, []int32{3, 5, 7}, func(w *BitWriter) { w.UBitVarFP(1); w.VarInt(-2) }, []int32{3, 3}},
		{opPopNAndNonTopological, []int32{3, 5, 7}, func(w *BitWriter) {
			w.UBitVarFP(1)
			w.Bit(true)
			w.VarInt(-1)
			w.Bit(true)
			w.VarInt(2)
		}, []int32{2, 7}},
		{opNonTopoComplex, []int32{3, 5}, func(w *BitWriter) { w.Bit(true); w.VarInt(-3); w.Bit(false) }, []int32{0, 5}},
		{opNonTopoPenultimatePlusOne, []int32{3, 5}, nil, []int32{4, 5}},
		{opNonTopoComplexPack4Bits, []int32{3, 5}, func(w *BitWriter) {
			w.Bit(true)
			w.Bits(9, 4) // +2
			w.Bit(true)
			w.Bits(5, 4) // -2
		}, []int32{5, 3}},
	}
	if len(tests) != int(numFieldPathOps)-1 {
		t.Fatalf("%d cases for %d ops", len(tests), numFieldPathOps-1)
	}
	for _, tc := range tests {
		var w BitWriter
		if tc.operands != nil {
			tc.operands(&w)
		}
		var p fieldPath
		copy(p.idx[:], tc.from)
		p.last = len(tc.from) - 1
		r := bitread.New(w.Data())
		if err := p.apply(tc.op, r); err != nil || r.Err() != nil {
			t.Errorf("op %d: %v, %v", tc.op, err, r.Err())
			continue
		}
		if got := p.idx[:p.last+1]; !slices.Equal(got, tc.want) {
			t.Errorf("op %d from %v: got %v, want %v", tc.op, tc.from, got, tc.want)
		}
		if left := r.BitsLeft(); left > 7 {
			t.Errorf("op %d: %d operand bits unread", tc.op, left)
		}
	}
}

func TestReadFieldPaths(t *testing.T) {
	want := [][]int{{0}, {1}, {9, 0, 2}, {9, 0, 3}, {9, 4}, {12}, {12, 1}, {3}}
	var w BitWriter
	w.FieldPaths(want...)
	w.Bits(0b101, 3) // what follows the list
	r := bitread.New(w.Data())
	paths, err := readFieldPaths(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got [][]int
	for _, p := range paths {
		var ints []int
		for _, i := range p.idx[:p.last+1] {
			ints = append(ints, int(i))
		}
		got = append(got, ints)
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("got %v, want %v", got, want)
	}
	if v := r.ReadBits(3); v != 0b101 {
		t.Errorf("list ended at the wrong bit: then read %b", v)
	}
}

func TestReadFieldPathsErrors(t *testing.T) {
	tests := []struct {
		name  string
		write func(w *BitWriter)
		want  string
	}{
		{"pop too far", func(w *BitWriter) { w.op(opPopNPlusOne); w.UBitVarFP(1) }, "pop 1 of 1"},
		{"too deep", func(w *BitWriter) {
			for range maxPathDepth {
				w.op(opPushOneLeftDeltaZeroRightZero)
			}
		}, "deeper than"},
		{"no penultimate", func(w *BitWriter) { w.op(opNonTopoPenultimatePlusOne) }, "penultimate"},
		{"truncated", func(w *BitWriter) { w.op(opPlusOne) }, "read past end"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var w BitWriter
			tc.write(&w)
			_, err := readFieldPaths(bitread.New(w.Data()), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error with %q", err, tc.want)
			}
		})
	}
}
