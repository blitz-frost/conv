// Package conv defines a framework for conversions between Go types. It is meant to ultimately serve as a compile time tool.
//
// This package explicitly imports all "reflect" identifiers.
//
// Builder, Scheme and Library are the core types of this package.
package conv

import (
	"errors"
	. "reflect"
	"sync"
)

var ErrInvalid = errors.New("invalid conversion")

// A Builder is used to obtain conversion functions for a particular type. It must return false if it cannot handle the input type.
// Multiple Builders should be used together, each one covering a different case, making code more modular.
// The Converter and Inverter types defined in this package are examples for the generic T.
type Builder[T any] func(Type) (T, bool)

// Converter is the standard conversion function type.
// Uses reflect.Value (instead of any) as the reflect package is very likely to be needed inside the function.
type Converter[T any] func(Value) (T, error)

// Inverter is the standard inversion function type.
type Inverter[T any] func(T) (Value, error)

// A Library wraps a Builder, caching build results for future reuse.
// This favors complex Builders that return optimized functions for a particular type, as the build time must only be spent once for each unique encountered type.
// Safe for concurrent use.
type Library[T any] struct {
	m   map[Type]T
	mux sync.RWMutex

	b    Builder[T]
	zero T // default value to use, if one cannot be built
}

// "zero" will be used as default when the wrapped builder doesn't cover a particular type.
func NewLibrary[T any](b Builder[T], zero T) *Library[T] {
	return &Library[T]{
		m:    make(map[Type]T),
		b:    b,
		zero: zero,
	}
}

// Get returns the cached function for type "t". If this is the first time that the type is encountered, builds and caches the return value first.
func (x *Library[T]) Get(t Type) T {
	x.mux.RLock()

	if o, ok := x.m[t]; ok {
		x.mux.RUnlock()
		return o
	}

	x.mux.RUnlock()
	x.mux.Lock()

	// check again, in case another goroutine locked just before this one, for the same reason
	if o, ok := x.m[t]; ok {
		x.mux.Unlock()
		return o
	}

	o, ok := x.b(t)
	if !ok {
		o = x.zero
	}
	x.m[t] = o

	x.mux.Unlock()

	return o
}

// A Conversion is a Library specialized in standard Converter functions (from multiple types to a specific one).
// Users can define their own Converter and Conversion variants, if the standard ones don't suit needs.
type Conversion[T any] Library[Converter[T]]

func NewConversion[T any](b Builder[Converter[T]]) *Conversion[T] {
	return (*Conversion[T])(NewLibrary[Converter[T]](b, converterInvalid[T]))
}

func (x *Conversion[T]) Call(v any) (T, error) {
	f := (*Library[Converter[T]])(x).Get(TypeOf(v))
	return f(ValueOf(v))
}

// A Inversion is a Library specialized in standard Inverter functions (from one specific type to multiple others).
type Inversion[T any] Library[Inverter[T]]

func NewInversion[T any](b Builder[Inverter[T]]) *Inversion[T] {
	return (*Inversion[T])(NewLibrary[Inverter[T]](b, inverterInvalid[T]))
}

// As is the equivalent of the Conversion.Call method, but Go methods cannot currently take type parameters.
func As[S any, T any](x *Inversion[T], v T) (S, error) {
	t := TypeOf((*S)(nil)).Elem()
	f := (*Library[Inverter[T]])(x).Get(t)
	ov, err := f(v)
	if err != nil {
		var o S
		return o, err
	}
	return ov.Interface().(S), nil
}

// A Scheme is a collection of Builders to be used together.
// Each member will be called sequentially, in the order they were added, until one return true.
type Scheme[T any] []Builder[T]

func (x Scheme[T]) Build(t Type) (T, bool) {
	for _, b := range x {
		if o, ok := b(t); ok {
			return o, true
		}
	}
	var o T
	return o, false
}

func (x *Scheme[T]) Use(b Builder[T]) {
	*x = append(*x, b)
}

// Check applies "fn" recursively to composite types.
// Returns on the first false.
func Check(t Type, fn func(Type) bool) bool {
	if !fn(t) {
		return false
	}

	switch t.Kind() {
	case Array, Chan, Pointer, Slice:
		return fn(t.Elem())
	case Map:
		if !fn(t.Key()) {
			return false
		}
		return fn(t.Elem())
	case Struct:
		for i, n := 0, t.NumField(); i < n; i++ {
			if !fn(t.Field(i).Type) {
				return false
			}
		}
	case Func:
		for i, n := 0, t.NumIn(); i < n; i++ {
			if !fn(t.In(i)) {
				return false
			}
		}
		for i, n := 0, t.NumOut(); i < n; i++ {
			if !fn(t.Out(i)) {
				return false
			}
		}
	}

	return true
}

// TypeEval returns the reflect.Type of a generic T.
func TypeEval[T any]() Type {
	return TypeOf((*T)(nil)).Elem()
}

func converterInvalid[T any](v Value) (T, error) {
	var o T
	return o, ErrInvalid
}

func inverterInvalid[T any](v T) (Value, error) {
	return Value{}, ErrInvalid
}

/*
func Implicit(tIn, tOut Type) (Converter, bool) {
	if tIn.AssignableTo(tOut) {
		return func(v Value) (Value, error) {
			o := New(tOut).Elem()
			o.Set(v)
			return o, nil
		}, true
	}

	if tIn.ConvertibleTo(tOut) {
		return func(v Value) (Value, error) {
			o := v.Convert(tOut)
			return o, nil
		}, true
	}

	// check here, because the standard Go logic should already cover most common cases
	if !canImplicit(tIn, tOut) {
		return nil, false
	}

	switch tIn.Kind() {
	case Func, Pointer:
		// can directly get unsafe.Pointer of these types
		return func(v Value) (Value, error) {
			ptr := v.UnsafePointer()
			o := NewAt(tOut, ptr).Elem()
			return o, nil
		}, true
	}

	// unaddressable types must be copied
	return func(v Value) (Value, error) {
		oPtr := New(tIn)
		oPtr.Elem().Set(v)
		ptr := oPtr.UnsafePointer()
		o := NewAt(tOut, ptr).Elem()
		return o, nil
	}, true
}

// canImplicit returns true if tIn and tOut share the same memory representation
func canImplicit(tIn, tOut Type) bool {
	k := tIn.Kind()
	if k == Interface {
		return false
	}
	if k != tOut.Kind() {
		return false
	}

	switch k {
	case Array:
		if tIn.Len() != tOut.Len() {
			return false
		}
		return canImplicit(tIn.Elem(), tOut.Elem())
	case Chan:
		if tIn.ChanDir() != tOut.ChanDir() {
			return false
		}
		return canImplicit(tIn.Elem(), tOut.Elem())
	case Func:
		nIn := tIn.NumIn()
		nOut := tIn.NumOut()
		if nIn != tOut.NumIn() || nOut != tOut.NumOut() {
			return false
		}
		for i := 0; i < nIn; i++ {
			if !canImplicit(tIn.In(i), tOut.In(i)) {
				return false
			}
		}
		for i := 0; i < nOut; i++ {
			if !canImplicit(tIn.Out(i), tOut.Out(i)) {
				return false
			}
		}
	case Map:
		if !canImplicit(tIn.Key(), tOut.Key()) {
			return false
		}
		fallthrough
	case Pointer:
		fallthrough
	case Slice:
		return canImplicit(tIn.Elem(), tOut.Elem())
	case Struct:
		n := tIn.NumField()
		if n != tOut.NumField() {
			return false
		}
		for i := 0; i < n; i++ {
			if !canImplicit(tIn.Field(i).Type, tOut.Field(i).Type) {
				return false
			}
		}
	}

	return true
}
*/
