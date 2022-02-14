package conv

import (
	"hash/maphash"
	"reflect"
	"strconv"
	"sync"

	"github.com/blitz-frost/conv/numeric"
)

// isBasic returns true if t is an undefined type, either simple or composed of only undefined types.
// Interfaces or types composed of interfaces are not basic.
func isBasic(t reflect.Type) bool {
	if t.PkgPath() != "" {
		return false
	}

	k := t.Kind()

	if _, ok := simpleTypes[k]; ok {
		return true
	}

	switch k {
	case reflect.Array:
		fallthrough
	case reflect.Slice:
		fallthrough
	case reflect.Ptr:
		return isBasic(t.Elem())
	case reflect.Map:
		return isBasic(t.Key()) && isBasic(t.Elem())
	case reflect.Struct:
		for i, n := 0, t.NumField(); i < n; i++ {
			if !isBasic(t.Field(i).Type) {
				return false
			}
		}
		return true
	case reflect.Func:
		for i, n := 0, t.NumIn(); i < n; i++ {
			if !isBasic(t.In(i)) {
				return false
			}
		}
		for i, n := 0, t.NumOut(); i < n; i++ {
			if !isBasic(t.Out(i)) {
				return false
			}
		}
		return true
	}

	return false
}

var (
	hash    maphash.Hash
	hashMux sync.Mutex
)

// A base describes the memory representation of a type.
// The zero base is invalid.
//BUG arrays and structs with more than 255 elements are currently not supported.
type base []byte

func baseOf(t reflect.Type) base {
	var x []byte

	k := t.Kind()
	k = numeric.Alias(k)

	x = append(x, byte(k))

	if _, ok := simpleTypes[k]; ok {
		return x
	}

	switch k {
	case reflect.Array:
		// arrays are followed by their length
		x = append(x, byte(t.Len()))
		fallthrough
	case reflect.Slice:
		fallthrough
	case reflect.Ptr:
		x = append(x, baseOf(t.Elem())...)
	case reflect.Map:
		x = append(x, baseOf(t.Key())...)
		x = append(x, baseOf(t.Elem())...)
	case reflect.Struct:
		// structs are followed by field number
		n := t.NumField()
		x = append(x, byte(n))
		for i := 0; i < n; i++ {
			x = append(x, baseOf(t.Field(i).Type)...)
		}
	case reflect.Chan:
		// channels are followed by direction
		x = append(x, byte(t.ChanDir()))
		x = append(x, baseOf(t.Elem())...)
	case reflect.Func:
		// functions are followed by input and output numbers, as well as variadic specifier
		ni := t.NumIn()
		no := t.NumOut()
		x = append(x, byte(ni))
		x = append(x, byte(no))
		if t.IsVariadic() {
			x = append(x, 1)
		} else {
			x = append(x, 0)
		}
		for i := 0; i < ni; i++ {
			x = append(x, baseOf(t.In(i))...)
		}
		for i := 0; i < no; i++ {
			x = append(x, baseOf(t.Out(i))...)
		}
	}

	return x
}

// asType returns the type corresponding to this base
func (x base) asType() reflect.Type {
	t, _ := x.asTypeRec()
	return t
}

// asTypeRec is the recursive version of "asType".
// It additionally returns how much of the base has been consumed.
func (x base) asTypeRec() (reflect.Type, int) {
	k := reflect.Kind(x[0])

	if simple, ok := simpleTypes[k]; ok {
		return simple, 1
	}

	switch k {
	case reflect.Array:
		n := int(x[1])
		elem, c := x[2:].asTypeRec()
		return reflect.ArrayOf(n, elem), 2 + c
	case reflect.Chan:
		dir := reflect.ChanDir(x[1])
		elem, c := x[2:].asTypeRec()
		return reflect.ChanOf(dir, elem), 2 + c
	case reflect.Func:
		in := make([]reflect.Type, x[1])
		out := make([]reflect.Type, x[2])
		variadic := false
		if x[3] == 1 {
			variadic = true
		}
		i := 4 // base index
		for j := 0; j < len(in); j++ {
			elem, c := x[i:].asTypeRec()
			in[j] = elem
			i += c
		}
		for j := 0; j < len(out); j++ {
			elem, c := x[i:].asTypeRec()
			out[j] = elem
			i += c
		}
		return reflect.FuncOf(in, out, variadic), i
	case reflect.Map:
		i := 1
		key, c := x[i:].asTypeRec()
		i += c
		elem, c := x[i:].asTypeRec()
		i += c
		return reflect.MapOf(key, elem), i
	case reflect.Slice:
		elem, c := x[1:].asTypeRec()
		return reflect.SliceOf(elem), 1 + c
	case reflect.Struct:
		fields := make([]reflect.StructField, x[1])
		i := 1
		for j := 0; j < len(fields); j++ {
			t, c := x[i:].asTypeRec()
			fields[i] = reflect.StructField{
				Name: "F" + strconv.Itoa(j),
				Type: t,
			}
			i += c
		}
		return reflect.StructOf(fields), i
	}

	return nil, 0
}

func (x base) hash() uint64 {
	hashMux.Lock()
	defer hashMux.Unlock()

	hash.Write([]byte(x))
	o := hash.Sum64()
	hash.Reset()
	return o
}

// isConcrete returns true if the base does not contain any interfaces.
func (x base) isConcrete() bool {
	for i := 0; i < len(x); i++ {
		switch reflect.Kind(x[i]) {
		case reflect.Interface:
			return false
		case reflect.Array:
			fallthrough
		case reflect.Struct:
			i++ // skip length
		case reflect.Func:
			i += 2 // skip input and output lengths
		}
	}

	return true
}
