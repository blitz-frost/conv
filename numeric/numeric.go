package numeric

import (
	"errors"
	"reflect"
)

var (
	kindInt  reflect.Kind
	kindUint reflect.Kind
)

func init() {
	if arch == 64 {
		kindInt = reflect.Int64
		kindUint = reflect.Uint64
	} else {
		kindInt = reflect.Int32
		kindUint = reflect.Uint32
	}
}

// Alias returns the corresponding alias for int and uint.
// Otherwise returns k unchanged.
func Alias(k reflect.Kind) reflect.Kind {
	switch k {
	case reflect.Int:
		return kindInt
	case reflect.Uint:
		return kindUint
	}

	return k
}

var arch = 8 * Types[reflect.Int].Align()

// Arch returns the int size in bits
func Arch() int {
	return arch
}

// basic type for each kind
var Types = map[reflect.Kind]reflect.Type{
	reflect.Int8:       reflect.TypeOf(int8(0)),
	reflect.Int16:      reflect.TypeOf(int16(0)),
	reflect.Int32:      reflect.TypeOf(int32(0)),
	reflect.Int64:      reflect.TypeOf(int64(0)),
	reflect.Int:        reflect.TypeOf(0),
	reflect.Uint8:      reflect.TypeOf(uint8(0)),
	reflect.Uint16:     reflect.TypeOf(uint16(0)),
	reflect.Uint32:     reflect.TypeOf(uint32(0)),
	reflect.Uint64:     reflect.TypeOf(uint64(0)),
	reflect.Uint:       reflect.TypeOf(uint(0)),
	reflect.Float32:    reflect.TypeOf(float32(0)),
	reflect.Float64:    reflect.TypeOf(float64(0)),
	reflect.Complex64:  reflect.TypeOf(complex64(0)),
	reflect.Complex128: reflect.TypeOf(complex128(0)),
}

type Nature int

const (
	Uint Nature = iota
	Int
	Float
	Complex
)

type Descriptor struct {
	Size   int
	Nature Nature
}

var Descriptors = map[reflect.Kind]Descriptor{
	reflect.Uint8:      Descriptor{1, Uint},
	reflect.Uint16:     Descriptor{2, Uint},
	reflect.Uint32:     Descriptor{4, Uint},
	reflect.Uint64:     Descriptor{8, Uint},
	reflect.Int8:       Descriptor{1, Int},
	reflect.Int16:      Descriptor{2, Int},
	reflect.Int32:      Descriptor{4, Int},
	reflect.Int64:      Descriptor{8, Int},
	reflect.Float32:    Descriptor{4, Float},
	reflect.Float64:    Descriptor{8, Float},
	reflect.Complex64:  Descriptor{8, Complex},
	reflect.Complex128: Descriptor{16, Complex},
}

var ratings = make(map[reflect.Kind]map[reflect.Kind]int, 14)

func init() {
	n := arch / 8
	Descriptors[reflect.Uint] = Descriptor{n, Uint}
	Descriptors[reflect.Int] = Descriptor{n, Int}

	// group by nature
	nats := make(map[Nature]map[int]struct{})
	// first group by nature
	for _, d := range Descriptors {
		if nats[d.Nature] == nil {
			nats[d.Nature] = make(map[int]struct{})
		}

		nats[d.Nature][d.Size] = struct{}{}
	}

	for k0, d0 := range Descriptors {
		ratings[k0] = make(map[reflect.Kind]int, 14)

		for k1, d1 := range Descriptors {
			c := 1
			if d0.Nature == Complex && d1.Nature < Float {
				c = 2 // complex only counts as half size for integer conversions
			}
			if d0.Nature < d1.Nature || (d0.Nature == d1.Nature && d0.Size/c < d1.Size) || (d0.Nature > d1.Nature && d0.Size/c <= d1.Size) {
				ratings[k0][k1] = -1
				continue
			}

			r := 0
			for i := d1.Nature; i < d0.Nature; i++ {
				for s, _ := range nats[i] {
					if s > d1.Size {
						r++
					}
				}
			}
			for s, _ := range nats[d0.Nature] {
				if s > d1.Size && s <= d0.Size/c {
					r++
				}
			}

			ratings[k0][k1] = r
		}
	}
}

func RateConversion(dst, src reflect.Kind) int {
	return ratings[dst][src]
}

// Converts src to dst, as per the rules of this package.
// dst must be a pointer to a numeric kind.
// src must be a numeric kind that can be converted losslessly to dst.
func Convert(dst, src interface{}) error {
	if dst == nil || src == nil {
		return errors.New("nil input")
	}

	dstVal := reflect.ValueOf(dst)
	if dstVal.Kind() != reflect.Ptr {
		return errors.New("non-pointer destination")
	}
	dstVal = dstVal.Elem()
	dstKind := dstVal.Kind()
	if _, ok := Types[dstKind]; !ok {
		return errors.New("non-numeric destination")
	}

	srcVal := reflect.ValueOf(src)
	srcKind := srcVal.Kind()
	if _, ok := Types[srcKind]; !ok {
		return errors.New("non-numeric source")
	}

	if RateConversion(dstKind, srcKind) == -1 {
		return errors.New("invalid conversion")
	}

	// cannot directly convert from non-complex to complex
	if dstKind == reflect.Complex128 && srcKind != reflect.Complex64 {
		f := srcVal.Convert(Types[reflect.Float64]).Float()
		srcVal = reflect.ValueOf(complex(f, 0))
	} else if dstKind == reflect.Complex64 {
		f := srcVal.Convert(Types[reflect.Float32]).Interface().(float32)
		srcVal = reflect.ValueOf(complex(f, 0))
	}

	dstVal.Set(srcVal.Convert(dstVal.Type()))
	return nil
}
