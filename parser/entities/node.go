package entities

import (
	"fmt"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
)

// node holds the values of one serializer or array: slot i is field i of
// the serializer, or element i of the array. kids holds the nested node of
// each Pointer, Table or array slot that has one, and is nil while none
// does.
type node struct {
	vals []Value
	kids []*node
}

func newNode(n int) *node { return &node{vals: make([]Value, n)} }

// clone returns a deep copy of n.
func (n *node) clone() *node {
	c := &node{vals: make([]Value, len(n.vals))}
	copy(c.vals, n.vals)
	if n.kids != nil {
		c.kids = make([]*node, len(n.kids))
		for i, k := range n.kids {
			if k != nil {
				c.kids[i] = k.clone()
			}
		}
	}
	return c
}

// assign makes n a deep copy of src, reusing n's storage where it can.
func (n *node) assign(src *node) {
	n.vals = append(n.vals[:0], src.vals...)
	if src.kids == nil {
		n.kids = nil
		return
	}
	if cap(n.kids) < len(src.kids) {
		n.kids = make([]*node, len(src.kids))
	}
	n.kids = n.kids[:len(src.kids)]
	for i, k := range src.kids {
		switch {
		case k == nil:
			n.kids[i] = nil
		case n.kids[i] == nil:
			n.kids[i] = k.clone()
		default:
			n.kids[i].assign(k)
		}
	}
}

// resize sets n's slot count to size, dropping slots past it.
func (n *node) resize(size int) {
	if size <= len(n.vals) {
		clear(n.vals[size:])
		n.vals = n.vals[:size]
		if n.kids != nil {
			clear(n.kids[size:])
			n.kids = n.kids[:size]
		}
		return
	}
	n.vals = append(n.vals, make([]Value, size-len(n.vals))...)
	if n.kids != nil {
		n.kids = append(n.kids, make([]*node, size-len(n.kids))...)
	}
}

// kid returns the nested node in slot i, which holds field f, making it
// if needed.
func (n *node) kid(i int, f *Field) *node {
	if n.kids == nil {
		n.kids = make([]*node, len(n.vals))
	}
	k := n.kids[i]
	if k == nil {
		k = newNode(nestedSize(f, n.vals[i]))
		n.kids[i] = k
	}
	return k
}

// nestedSize returns the slot count of the node nested in a slot that
// holds field f and value v.
func nestedSize(f *Field, v Value) int {
	switch f.Decoder.Kind {
	case FixedArray:
		return f.Decoder.Count
	case DynamicArray:
		return int(v.u)
	case Pointer, Table:
		return len(nestedSerializer(f, v).Fields)
	}
	return 0
}

// nestedSerializer returns the serializer a Pointer or Table slot holds.
// A pointer's value is 0 when null, or 1 plus the index of its serializer
// in Serializer and then Polymorphic.
func nestedSerializer(f *Field, v Value) *Serializer {
	if v.u >= 2 && int(v.u-2) < len(f.Polymorphic) {
		return f.Polymorphic[v.u-2]
	}
	return f.Serializer
}

// readFields reads a field path list and then a value for each path, into
// the state n of an entity whose serializer is s. paths is scratch space,
// returned for reuse.
func readFields(r *bitread.Reader, s *Serializer, n *node, paths []fieldPath) ([]fieldPath, error) {
	paths, err := readFieldPaths(r, paths[:0])
	if err != nil {
		return paths, err
	}
	for i := range paths {
		if err := set(r, s, n, &paths[i]); err != nil {
			return paths, err
		}
	}
	return paths, r.Err()
}

// set reads the value at path p into the state n of an entity whose
// serializer is s.
func set(r *bitread.Reader, s *Serializer, n *node, p *fieldPath) error {
	i := int(p.idx[0])
	if i < 0 || i >= len(s.Fields) {
		return pathErr(p, 0, "%s has %d fields", s.Name, len(s.Fields))
	}
	f := s.Fields[i]
	for d := 1; d <= p.last; d++ {
		next := int(p.idx[d])
		switch f.Decoder.Kind {
		case Pointer, Table:
			ser := nestedSerializer(f, n.vals[i])
			if next < 0 || next >= len(ser.Fields) {
				return pathErr(p, d, "%s has %d fields", ser.Name, len(ser.Fields))
			}
			n, f = n.kid(i, f), ser.Fields[next]
		case FixedArray, DynamicArray:
			size := maxArrayLen
			if f.Decoder.Kind == FixedArray {
				size = f.Decoder.Count
			}
			if next < 0 || next >= size {
				return pathErr(p, d, "%s has no element %d", f.FullName(), next)
			}
			n, f = n.kid(i, f), f.Elem
			if next >= len(n.vals) {
				n.resize(next + 1)
			}
		default:
			return pathErr(p, d, "%s is a %s, not a struct or array", f.FullName(), f.Decoder)
		}
		i = next
	}

	switch f.Decoder.Kind {
	case Unknown, FixedArray, Table:
		return pathErr(p, p.last, "%s: can't read a %s value", f.FullName(), f.Decoder)
	}
	v := readValue(r, f)
	switch k := f.Decoder.Kind; {
	case k == DynamicArray && v.u > maxArrayLen:
		return pathErr(p, p.last, "%s: length %d", f.FullName(), v.u)
	case k == Pointer && v.u > uint64(len(f.Polymorphic))+1:
		return pathErr(p, p.last, "%s: type %d of %d", f.FullName(), v.u-1, len(f.Polymorphic)+1)
	}
	old := n.vals[i]
	n.vals[i] = v
	if n.kids == nil || n.kids[i] == nil {
		return nil
	}
	switch f.Decoder.Kind {
	case DynamicArray:
		n.kids[i].resize(int(v.u))
	case Pointer:
		if v.u == 0 || nestedSerializer(f, v) != nestedSerializer(f, old) {
			n.kids[i] = nil
		}
	}
	return nil
}

// maxArrayLen bounds array lengths and indices, so that corrupt data can't
// make huge allocations.
const maxArrayLen = 1 << 14

func pathErr(p *fieldPath, depth int, format string, args ...any) error {
	return fmt.Errorf("field path %v at index %d: %s", p.idx[:p.last+1], depth, fmt.Sprintf(format, args...))
}
