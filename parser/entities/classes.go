package entities

import (
	"fmt"
	"iter"
	"math/bits"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// Class is an entity class.
type Class struct {
	ID         int
	Name       string // network name, such as "CCSPlayerPawn"
	Serializer *Serializer
}

// Classes is the set of entity classes from one DEM_ClassInfo. Class ids
// run from 0 to Len()-1.
type Classes struct {
	list   []Class // by id
	byName map[string]*Class
}

// NewClasses builds the classes in m and gives each its serializer from s.
// DEM_SendTables comes before DEM_ClassInfo, so s is ready by then. A class
// id that isn't unique or isn't below the class count, a repeated name, or
// a class with no serializer gives an error wrapping demo.ErrBadDemo.
func NewClasses(m *pb.CDemoClassInfo, s *Serializers) (*Classes, error) {
	n := len(m.GetClasses())
	c := &Classes{list: make([]Class, n), byName: make(map[string]*Class, n)}
	for _, pc := range m.GetClasses() {
		id, name := int(pc.GetClassId()), pc.GetNetworkName()
		switch {
		case pc.ClassId == nil:
			return nil, classErr("class %q has no id", name)
		case id < 0 || id >= n:
			return nil, classErr("class %q: id %d is outside 0 to %d", name, id, n-1)
		case c.list[id].Serializer != nil:
			return nil, classErr("class id %d is both %q and %q", id, c.list[id].Name, name)
		case c.byName[name] != nil:
			return nil, classErr("class %q has ids %d and %d", name, c.byName[name].ID, id)
		}
		ser := s.Lookup(name)
		if ser == nil {
			return nil, classErr("class %q (id %d) has no serializer", name, id)
		}
		c.list[id] = Class{ID: id, Name: name, Serializer: ser}
		c.byName[name] = &c.list[id]
	}
	return c, nil
}

// Len returns the number of classes.
func (c *Classes) Len() int { return len(c.list) }

// IDBits returns the width in bits of a class id in entity data: enough
// for the class count, as floor(log2(Len)) + 1.
func (c *Classes) IDBits() int { return bits.Len(uint(len(c.list))) }

// ByID returns the class with the given id, or nil.
func (c *Classes) ByID(id int) *Class {
	if id < 0 || id >= len(c.list) {
		return nil
	}
	return &c.list[id]
}

// ByName returns the class with the given network name, or nil.
func (c *Classes) ByName(name string) *Class { return c.byName[name] }

// All yields every class in id order.
func (c *Classes) All() iter.Seq[*Class] {
	return func(yield func(*Class) bool) {
		for i := range c.list {
			if !yield(&c.list[i]) {
				return
			}
		}
	}
}

func classErr(format string, args ...any) error {
	return fmt.Errorf("%w: class info: %s", demo.ErrBadDemo, fmt.Sprintf(format, args...))
}
