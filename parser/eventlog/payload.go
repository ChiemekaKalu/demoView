package eventlog

import (
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Attr is one field of a payload: a key and its value. Only the functions
// below make one, so a payload holds integers, strings, booleans, players,
// objects and lists of them, and never a float. The zero Attr is invalid.
type Attr struct {
	key   string
	kind  kind
	num   int    // an integer, a boolean (0 or 1) or a player key
	str   string // a string
	attrs []Attr // an object's fields, or a list's items (with no keys)
}

type kind uint8

const (
	_ kind = iota
	kindInt
	kindString
	kindBool
	kindPlayer
	kindObject
	kindList
)

// Int returns an integer field.
func Int(key string, n int) Attr { return Attr{key: key, kind: kindInt, num: n} }

// String returns a string field.
func String(key, s string) Attr { return Attr{key: key, kind: kindString, str: s} }

// Bool returns a boolean field.
func Bool(key string, b bool) Attr {
	a := Attr{key: key, kind: kindBool}
	if b {
		a.num = 1
	}
	return a
}

// Player returns a field naming a player by a provisional key, which
// Resolver.Player turns into an idx. A negative key means no player, and is
// written as null.
func Player(key string, p int) Attr { return Attr{key: key, kind: kindPlayer, num: p} }

// Object returns a field holding an object with the given fields, in order.
func Object(key string, attrs ...Attr) Attr {
	return Attr{key: key, kind: kindObject, attrs: attrs}
}

// Ints returns a list of integers.
func Ints(key string, ns ...int) Attr {
	return list(key, ns, func(n int) Attr { return Int("", n) })
}

// Strings returns a list of strings.
func Strings(key string, ss ...string) Attr {
	return list(key, ss, func(s string) Attr { return String("", s) })
}

// Players returns a list of players, each as Player writes it.
func Players(key string, ps ...int) Attr {
	return list(key, ps, func(p int) Attr { return Player("", p) })
}

// Objects returns a list of objects.
func Objects(key string, objs ...[]Attr) Attr {
	return list(key, objs, func(o []Attr) Attr { return Object("", o...) })
}

func list[T any](key string, vs []T, item func(T) Attr) Attr {
	items := make([]Attr, len(vs))
	for i, v := range vs {
		items[i] = item(v)
	}
	return Attr{key: key, kind: kindList, attrs: items}
}

// appendObject appends attrs to l's payloads as a JSON object. A player
// becomes a hole for Finalize to fill.
func (l *layer) appendObject(attrs []Attr) error {
	l.data = append(l.data, '{')
	for i, a := range attrs {
		for _, prev := range attrs[:i] {
			if prev.key == a.key {
				return fmt.Errorf("payload key %q appears twice", a.key)
			}
		}
		if i > 0 {
			l.data = append(l.data, ',')
		}
		l.data = appendString(l.data, a.key)
		l.data = append(l.data, ':')
		if err := l.appendValue(a); err != nil {
			return err
		}
	}
	l.data = append(l.data, '}')
	return nil
}

func (l *layer) appendValue(a Attr) error {
	switch a.kind {
	case kindInt:
		l.data = strconv.AppendInt(l.data, int64(a.num), 10)
	case kindString:
		l.data = appendString(l.data, a.str)
	case kindBool:
		l.data = strconv.AppendBool(l.data, a.num != 0)
	case kindPlayer:
		switch {
		case a.num < 0:
			l.data = append(l.data, "null"...)
		case !fits32(a.num):
			return fmt.Errorf("player key %d is out of range", a.num)
		default:
			l.holes = append(l.holes, hole{off: int32(len(l.data)), key: int32(a.num)})
		}
	case kindObject:
		return l.appendObject(a.attrs)
	case kindList:
		l.data = append(l.data, '[')
		for i, item := range a.attrs {
			if i > 0 {
				l.data = append(l.data, ',')
			}
			if err := l.appendValue(item); err != nil {
				return err
			}
		}
		l.data = append(l.data, ']')
	default:
		return errors.New("payload holds a zero Attr")
	}
	return nil
}

// appendString appends s as a JSON string. Invalid UTF-8 becomes U+FFFD, so
// the output is always valid JSON.
func appendString(b []byte, s string) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				b = append(b, s[start:i]...)
				b = append(b, '\\', 'u', 'f', 'f', 'f', 'd')
				start = i + 1
			}
			i += size
			continue
		}
		if c >= ' ' && c != '"' && c != '\\' {
			i++
			continue
		}
		b = append(b, s[start:i]...)
		switch c {
		case '"', '\\':
			b = append(b, '\\', c)
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		}
		i++
		start = i
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}
