// Some generic type methods take pointers to distinguish their use case, not because it would be strictly necessary.

package conv

import (
	"reflect"
)

const reflectNumeric reflect.Kind = reflect.UnsafePointer + 1 // reflect.Kind corresponding to a generic numeric value

// A Map represents a generic map type.
type Map struct {
	v reflect.Value
}

func makeMapVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(Map{v})
}

func newMapVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(&Map{v})
}

func (x *Map) Clear() {
	x.v.Set(reflect.MakeMap(x.v.Type()))
}

func (x *Map) Delete(key interface{}) {
	x.v.SetMapIndex(reflect.ValueOf(key), reflect.Zero(x.v.Type().Elem()))
}

func (x Map) Key(k interface{}) interface{} {
	return x.v.MapIndex(reflect.ValueOf(k)).Interface()
}

func (x Map) Len() int {
	return x.v.Len()
}

func (x Map) Range() MapIter {
	return MapIter{x.v.MapRange()}
}

func (x *Map) Set(key, val interface{}) {
	x.v.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(val))
}

// A MapIter is used to iterate over a Map's key-values.
type MapIter struct {
	v *reflect.MapIter
}

func (x MapIter) Key() interface{} {
	return x.v.Key().Interface()
}

// Next must be called to advance through the map.
// Returns true if it finds a key-value pair, in which case the Key and Value methods may be used to retrieve them before the next Next call.
func (x MapIter) Next() bool {
	return x.v.Next()
}

func (x MapIter) Value() interface{} {
	return x.v.Value().Interface()
}

// A Number represents a generic numeric type.
type Number struct {
	v reflect.Value // underlying value
}

func makeNumberVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(Number{v})
}

func newNumberVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(&Number{v})
}

func (x *Number) Addr() interface{} {
	return x.v.Addr().Interface()
}

func (x *Number) Set(v interface{}) {
	x.v.Set(reflect.ValueOf(v))
}

func (x Number) Value() interface{} {
	return x.v.Interface()
}

// A Pointer represents a generic pointer type.
type Pointer struct {
	v reflect.Value // underlying value
}

func makePointerVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(Pointer{v})
}

func newPointerVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(&Pointer{v})
}

// Set sets the value being pointed at.
func (x *Pointer) Set(v interface{}) {
	x.v.Elem().Set(reflect.ValueOf(v))
}

// Value returns the value being pointed at.
func (x Pointer) Value() interface{} {
	return x.v.Elem().Interface()
}

// A Slice represents a generic slice or array.
type Slice struct {
	v    reflect.Value // underlying value
	elem reflect.Type
}

func makeSlice(v reflect.Value) Slice {
	return Slice{
		v:    v,
		elem: v.Type().Elem(),
	}
}

func makeSliceVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(makeSlice(v))
}

func newSliceVal(v reflect.Value) reflect.Value {
	x := makeSlice(v)
	return reflect.ValueOf(&x)
}

func (x *Slice) Append(v ...interface{}) {
	x.v.Set(reflect.AppendSlice(x.v, reflect.ValueOf(v)))
}

func (x *Slice) AppendPtr(v interface{}) {
	x.v.Set(reflect.Append(x.v, reflect.ValueOf(v).Elem()))
}

func (x *Slice) Clear() {
	x.v.Set(reflect.MakeSlice(x.v.Type(), 0, 0))
}

// Index returns the value at index i.
func (x Slice) Index(i int) interface{} {
	return x.v.Index(i).Interface()
}

func (x Slice) Len() int {
	return x.v.Len()
}

// New returns a new pointer to the element type.
func (x Slice) New() interface{} {
	return reflect.New(x.elem).Interface()
}

func (x *Slice) Set(i int, v interface{}) {
	x.v.Index(i).Set(reflect.ValueOf(v))
}

func (x *Slice) SetPtr(i int, v interface{}) {
	x.v.Index(i).Set(reflect.ValueOf(v).Elem())
}

// A Struct represents a generic struct type. It allows access only to exported fields.
// Embedded structs are flattened and become invisible. Fields of embedded structs with conflicting names will be invisible.
type Struct struct {
	v reflect.Value
}

func makeStructVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(Struct{v})
}

func newStructVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(&Struct{v})
}

func (x Struct) Field(name string) interface{} {
	t := x.v.Type()
	tf, ok := t.FieldByName(name)
	if !ok {
		return nil
	}
	if tf.Anonymous && tf.Type.Kind() == reflect.Struct {
		return nil
	}

	return x.v.FieldByIndex(tf.Index).Interface()
}

func (x Struct) Range() *StructIter {
	return newStructIter(x.v)
}

func (x *Struct) Set(field string, v interface{}) {
	x.v.FieldByName(field).Set(reflect.ValueOf(v))
}

// A StructIter is used to iterate over a struct's exported fields.
type StructIter struct {
	v     reflect.Value
	t     reflect.Type
	index [][]int
	i     int
	field reflect.StructField
}

func newStructIter(v reflect.Value) *StructIter {
	t := v.Type()
	visible := reflect.VisibleFields(t)
	var index [][]int
	for _, field := range visible {
		if !field.IsExported() {
			continue
		}
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			continue
		}

		index = append(index, field.Index)
	}

	return &StructIter{
		v:     v,
		t:     t,
		index: index,
		i:     -1,
	}
}

func (x StructIter) Name() string {
	return x.field.Name
}

// Next advances through the struct fields.
// It returns true if it finds an exported, unobscured field, in which case the Name, Value and Tag methods become available.
func (x *StructIter) Next() bool {
	x.i++
	if x.i >= len(x.index) {
		return false
	}

	x.field = x.t.FieldByIndex(x.index[x.i])
	return true
}

func (x StructIter) Tag() StructTag {
	return x.field.Tag
}

func (x StructIter) Value() interface{} {
	return x.v.FieldByIndex(x.index[x.i]).Interface()
}

type StructTag = reflect.StructTag
