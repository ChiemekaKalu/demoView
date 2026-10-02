package entities

import (
	"errors"
	"fmt"

	"github.com/ChiemekaKalu/demoview/parser/demo"
)

// ErrUnknown is wrapped by errors about a class, field or decoder this
// package doesn't know: entity data that sets a field with no decoder
// (Serializers.Unclassified), a create of a class id outside the class
// list, or a Ref to a field a class doesn't have. A game update that
// changes entities gives these. It wraps demo.ErrBadDemo.
var ErrUnknown = fmt.Errorf("%w: unknown entity class, field or decoder", demo.ErrBadDemo)

// unknownError is an error that matches ErrUnknown, and so demo.ErrBadDemo,
// without repeating ErrUnknown's text.
type unknownError struct{ msg string }

func (e unknownError) Error() string { return e.msg }

func (e unknownError) Is(target error) bool { return target == ErrUnknown || target == demo.ErrBadDemo }

func unknownf(format string, args ...any) error { return unknownError{fmt.Sprintf(format, args...)} }

// cause returns the sentinel an Apply error wraps: ErrUnknown if err is
// about something unknown, and demo.ErrBadDemo otherwise.
func cause(err error) error {
	if errors.Is(err, ErrUnknown) {
		return ErrUnknown
	}
	return demo.ErrBadDemo
}
