package entities

import (
	"strconv"
	"strings"
)

// Ref locates one field in the entities of one class. Resolve a Ref once,
// with Class.Ref, and read it from any number of entities and ticks with
// Entity.Get.
type Ref struct {
	ser    *Serializer
	path   fieldPath
	fields [maxPathDepth]*Field // the field at each path index
}

// Field returns the field r reads.
func (r Ref) Field() *Field { return r.fields[r.path.last] }

// Ref resolves a field of c's entities by name: the full names of the
// fields along its path, joined by dots, with array elements by number.
// For example, "m_iHealth",
// "CBodyComponent.m_skeletonInstance.m_vecOrigin.m_cellX" or
// "m_pWeaponServices.m_hMyWeapons.0". A polymorphic pointer resolves through
// its own serializer. A name that doesn't resolve gives an error matching
// ErrUnknown.
func (c *Class) Ref(name string) (Ref, error) {
	r := Ref{ser: c.Serializer}
	if !r.resolve(c.Serializer, name, 0) {
		return Ref{}, unknownf("entities: class %s has no field %q", c.Name, name)
	}
	return r, nil
}

// resolve finds name in s, at path index d, and the rest of it below.
func (r *Ref) resolve(s *Serializer, name string, d int) bool {
	if d == maxPathDepth {
		return false
	}
	for i, f := range s.Fields {
		full := f.FullName()
		rest, ok := strings.CutPrefix(name, full)
		if !ok || rest != "" && rest[0] != '.' {
			continue
		}
		r.path.idx[d], r.path.last, r.fields[d] = int32(i), d, f
		if r.below(f, strings.TrimPrefix(rest, "."), d) {
			return true
		}
	}
	return false
}

// below resolves name under field f at path index d; "" names f itself.
func (r *Ref) below(f *Field, name string, d int) bool {
	if name == "" {
		switch f.Decoder.Kind {
		case Unknown, FixedArray, Table:
			return false
		}
		return true
	}
	switch f.Decoder.Kind {
	case Pointer, Table:
		return r.resolve(f.Serializer, name, d+1)
	case FixedArray, DynamicArray:
		elem, rest, _ := strings.Cut(name, ".")
		i, err := strconv.Atoi(elem)
		if err != nil || i < 0 || i >= maxArrayLen || f.Decoder.Kind == FixedArray && i >= f.Decoder.Count || d+1 == maxPathDepth {
			return false
		}
		r.path.idx[d+1], r.path.last, r.fields[d+1] = int32(i), d+1, f.Elem
		return r.below(f.Elem, rest, d+1)
	}
	return false
}

// Get returns e's value of the field r locates. It reports false if r is
// for another class, or the value isn't there: it is under a null pointer,
// a pointer holding another type, or past the end of an array. A field
// that is there but was never sent holds the zero Value.
func (e *Entity) Get(r Ref) (Value, bool) {
	if e.Class.Serializer != r.ser || r.ser == nil {
		return Value{}, false
	}
	n := e.root
	for d := 0; d < r.path.last; d++ {
		i := int(r.path.idx[d])
		if i >= len(n.vals) || n.kids == nil || n.kids[i] == nil {
			return Value{}, false
		}
		if f := r.fields[d]; f.Decoder.Kind == Pointer {
			s, next := nestedSerializer(f, n.vals[i]), int(r.path.idx[d+1])
			if next >= len(s.Fields) || s.Fields[next] != r.fields[d+1] {
				return Value{}, false
			}
		}
		n = n.kids[i]
	}
	i := int(r.path.idx[r.path.last])
	if i >= len(n.vals) {
		return Value{}, false
	}
	return n.vals[i], true
}
