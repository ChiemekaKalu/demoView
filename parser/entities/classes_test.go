package entities_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// serializersFor returns serializers, with no fields, with the given names.
func serializersFor(t *testing.T, names ...string) *entities.Serializers {
	t.Helper()
	var b flat
	for _, n := range names {
		b.serializer(n, 0)
	}
	return b.build(t)
}

type class struct {
	id   int32
	name string
}

func classInfo(classes ...class) *pb.CDemoClassInfo {
	var m pb.CDemoClassInfo
	for _, c := range classes {
		m.Classes = append(m.Classes, &pb.CDemoClassInfoClassT{ClassId: proto.Int32(c.id), NetworkName: proto.String(c.name)})
	}
	return &m
}

func TestNewClasses(t *testing.T) {
	sers := serializersFor(t, "A", "B", "C", "Unused")
	c, err := entities.NewClasses(classInfo(class{2, "C"}, class{0, "A"}, class{1, "B"}), sers)
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 3 || c.IDBits() != 2 {
		t.Errorf("Len %d, IDBits %d; want 3, 2", c.Len(), c.IDBits())
	}
	var names []string
	for cl := range c.All() {
		names = append(names, cl.Name)
		if cl.Serializer != sers.Lookup(cl.Name) {
			t.Errorf("class %s has serializer %v", cl.Name, cl.Serializer)
		}
		if c.ByID(cl.ID) != cl || c.ByName(cl.Name) != cl {
			t.Errorf("class %s: ByID or ByName gives another class", cl.Name)
		}
	}
	if want := []string{"A", "B", "C"}; !slices.Equal(names, want) {
		t.Errorf("All = %v, want %v", names, want)
	}
	if c.ByID(-1) != nil || c.ByID(3) != nil || c.ByName("Unused") != nil {
		t.Error("found a class that isn't there")
	}
}

func TestIDBits(t *testing.T) {
	for n, want := range map[int]int{0: 0, 1: 1, 2: 2, 3: 2, 4: 3, 211: 8, 218: 8, 255: 8, 256: 9} {
		names := make([]string, n)
		classes := make([]class, n)
		for i := range n {
			names[i] = fmt.Sprint("C", i)
			classes[i] = class{int32(i), names[i]}
		}
		c, err := entities.NewClasses(classInfo(classes...), serializersFor(t, names...))
		if err != nil {
			t.Fatal(err)
		}
		if got := c.IDBits(); got != want {
			t.Errorf("%d classes: IDBits %d, want %d", n, got, want)
		}
	}
}

func TestNewClassesErrors(t *testing.T) {
	sers := serializersFor(t, "A", "B")
	noID := classInfo(class{0, "A"}, class{1, "B"})
	noID.Classes[1].ClassId = nil
	tests := []struct {
		name string
		info *pb.CDemoClassInfo
		want string
	}{
		{"id too big", classInfo(class{0, "A"}, class{2, "B"}), `class "B": id 2 is outside 0 to 1`},
		{"negative id", classInfo(class{0, "A"}, class{-1, "B"}), `class "B": id -1 is outside 0 to 1`},
		{"no id", noID, `class "B" has no id`},
		{"repeated id", classInfo(class{0, "A"}, class{0, "B"}), `class id 0 is both "A" and "B"`},
		{"repeated name", classInfo(class{0, "A"}, class{1, "A"}), `class "A" has ids 0 and 1`},
		{"no serializer", classInfo(class{0, "A"}, class{1, "Z"}), `class "Z" (id 1) has no serializer`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := entities.NewClasses(tc.info, sers)
			if !errors.Is(err, demo.ErrBadDemo) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want ErrBadDemo with %q", err, tc.want)
			}
		})
	}
}
