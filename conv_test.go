package conv

import (
	. "reflect"
	"testing"
)

func TestConversion(t *testing.T) {
	b := func(t Type) (Converter[int], bool) {
		if t.Kind() != Int {
			return nil, false
		}
		return func(v Value) (int, error) {
			return int(v.Int()), nil
		}, true
	}
	bb := func(t Type) (Converter[int], bool) {
		if t.Kind() != Slice || t.Elem().Kind() != Int {
			return nil, false
		}
		return func(v Value) (int, error) {
			o := 0
			for i, n := 0, v.Len(); i < n; i++ {
				o += int(v.Index(i).Int())
			}
			return o, nil
		}, true
	}

	scheme := Scheme[Converter[int]]{}
	scheme.Use(b)
	scheme.Use(bb)
	c := NewConversion(scheme.Build)

	type someInt int
	aInt := someInt(44)
	bInt, err := c.Call(aInt)
	if err != nil || bInt != 44 {
		t.Error("int failed", err)
	}

	bSlice, err := c.Call([]int{1, 2, 3})
	if err != nil || bSlice != 6 {
		t.Error("slice failed", err)
	}
}

func TestInversion(t *testing.T) {
	b := func(t Type) (Inverter[int], bool) {
		if t.Kind() != Int {
			return nil, false
		}
		return func(v int) (Value, error) {
			o := New(t).Elem()
			o.SetInt(int64(v))
			return o, nil
		}, true
	}
	bb := func(t Type) (Inverter[int], bool) {
		if t.Kind() != Slice || t.Elem().Kind() != Int {
			return nil, false
		}
		return func(v int) (Value, error) {
			o := MakeSlice(t, 1, 1)
			o.Index(0).SetInt(int64(v))
			return o, nil
		}, true
	}

	scheme := Scheme[Inverter[int]]{}
	scheme.Use(b)
	scheme.Use(bb)
	c := NewInversion(scheme.Build)

	type someInt int
	bInt, err := As[someInt, int](c, 44)
	if err != nil || bInt != 44 {
		t.Error("int failed", err)
	}

	bSlice, err := As[[]int, int](c, 44)
	if err != nil || len(bSlice) != 1 || bSlice[0] != 44 {
		t.Error("slice failed", err)
	}
}
