// Some generic type methods take pointers to distinguish their use case, not because it would be strictly necessary.

package conv

import (
	"reflect"
	"unsafe"
)

const reflectNumeric reflect.Kind = reflect.UnsafePointer + 1 // reflect.Kind corresponding to a generic numeric value

// fieldSet sets the value of a field by using its unsafe address.
// This circumvents exported field protection.
func fieldSet(field, val reflect.Value) {
	p := unsafe.Pointer(field.UnsafeAddr())
	fu := reflect.NewAt(field.Type(), p).Elem()
	fu.Set(val)
}

// unsafeAddr returns a value's address.
// Returns the address of a copy if v is not addressable.
func unsafeAddr(v reflect.Value) uintptr {
	if v.CanAddr() {
		return v.UnsafeAddr()
	}

	c := reflect.New(v.Type()).Elem()
	c.Set(v)
	return c.UnsafeAddr()
}

func unsafeSet(v reflect.Value, p unsafe.Pointer) {
	c := reflect.NewAt(v.Type(), p).Elem()
	v.Set(c)
}

type Array struct {
	v    reflect.Value // underlying value
	elem reflect.Type
}

func makeArray(v reflect.Value) Array {
	return Array{
		v:    v,
		elem: v.Type().Elem(),
	}
}

func makeArrayVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(makeArray(v))
}

func newArrayVal(v reflect.Value) reflect.Value {
	x := makeArray(v)
	return reflect.ValueOf(&x)
}

func (x Array) Elem() reflect.Type {
	return x.elem
}

// Index returns the value at index i.
func (x Array) Index(i int) any {
	return x.v.Index(i).Interface()
}

func (x Array) Len() int {
	return x.v.Len()
}

// New returns a new pointer to the element type.
func (x Array) New() any {
	return reflect.New(x.elem).Interface()
}

func (x *Array) Set(i int, v any) {
	x.SetValue(i, reflect.ValueOf(v))
}

func (x *Array) SetPtr(i int, v any) {
	x.SetValue(i, reflect.ValueOf(v).Elem())
}

func (x *Array) SetValue(i int, v reflect.Value) {
	x.v.Index(i).Set(v)
}

// Unsafe returns a pointer to the array.
// If the array is unaddressable, returns a pointer to a copy.
func (x Array) Unsafe() uintptr {
	return unsafeAddr(x.v)
}

// UnsafeSet copies arbitrary memory into the array.
// The length parameter is present to satisfy the ArrayPointerInterface, and is otherwise not used.
func (x *Array) UnsafeSet(p unsafe.Pointer, length int) {
	unsafeSet(x.v, p)
}

// Array and Slice are kept as separate generics, in order to clearly define their respective method sets.
// ArrayInterface facilitates code factoring.
type ArrayInterface interface {
	Elem() reflect.Type
	Index(int) any
	Len() int
	New() any
	Unsafe() uintptr
}

type ArrayPointerInterface interface {
	ArrayInterface
	Set(int, any)
	SetPtr(int, any)
	UnsafeSet(unsafe.Pointer, int)
}

// A Map represents a generic map type.
type Map struct {
	v    reflect.Value
	key  reflect.Type
	elem reflect.Type
}

func makeMap(v reflect.Value) Map {
	t := v.Type()
	return Map{
		v:    v,
		key:  t.Key(),
		elem: t.Elem(),
	}
}

func makeMapVal(v reflect.Value) reflect.Value {
	m := makeMap(v)
	return reflect.ValueOf(m)
}

func newMapVal(v reflect.Value) reflect.Value {
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}
	m := makeMap(v)
	return reflect.ValueOf(&m)
}

func (x *Map) Clear() {
	x.v.Set(reflect.MakeMap(x.v.Type()))
}

func (x *Map) Delete(key any) {
	x.v.SetMapIndex(reflect.ValueOf(key), reflect.Zero(x.v.Type().Elem()))
}

func (x Map) Key(k any) any {
	return x.v.MapIndex(reflect.ValueOf(k)).Interface()
}

func (x Map) Len() int {
	return x.v.Len()
}

func (x Map) NewKey() any {
	return reflect.New(x.key).Interface()
}

func (x Map) NewValue() any {
	return reflect.New(x.elem).Interface()
}

func (x Map) Range() MapIter {
	return MapIter{x.v.MapRange()}
}

func (x *Map) Set(key, val any) {
	x.SetValue(reflect.ValueOf(key), reflect.ValueOf(val))
}

func (x *Map) SetPtr(key, val any) {
	x.SetValue(reflect.ValueOf(key).Elem(), reflect.ValueOf(val).Elem())
}

func (x *Map) SetValue(key, val reflect.Value) {
	x.v.SetMapIndex(key, val)
}

// A MapIter is used to iterate over a Map's key-values.
type MapIter struct {
	v *reflect.MapIter
}

func (x MapIter) Key() any {
	return x.v.Key().Interface()
}

// Next must be called to advance through the map.
// Returns true if it finds a key-value pair, in which case the Key and Value methods may be used to retrieve them before the next Next call.
func (x MapIter) Next() bool {
	return x.v.Next()
}

func (x MapIter) Value() any {
	return x.v.Value().Interface()
}

// Nil can be used to define explicit handling of nil source values in schemes.
// Cannot be used by inverse schemes.
type Nil struct{}

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

func (x *Number) Addr() any {
	return x.v.Addr().Interface()
}

func (x *Number) Set(v any) {
	x.v.Set(reflect.ValueOf(v))
}

func (x Number) Size() uintptr {
	return x.v.Type().Size()
}

// Unsafe returns a pointer to the value held by the Number.
// If the value is unaddressable, returns a pointer to a copy.
// This method is for direct memory reading; for assignment, use UnsafeSet.
func (x Number) Unsafe() uintptr {
	return unsafeAddr(x.v)
}

// UnsafeSet assigns a copy of arbitrary memory, starting at p, to the Number value.
func (x *Number) UnsafeSet(p unsafe.Pointer) {
	unsafeSet(x.v, p)
}

func (x Number) Value() any {
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
	// ensure pointers of nilable types are allocated
	if v.IsNil() {
		n := reflect.New(v.Type().Elem())
		v.Set(n)
	}

	return reflect.ValueOf(&Pointer{v})
}

// Elem returns the value being pointed at.
func (x Pointer) Elem() any {
	return x.v.Elem().Interface()
}

// ElemSet sets the value being pointed at.
func (x *Pointer) ElemSet(v any) {
	x.v.Elem().Set(reflect.ValueOf(v))
}

// New returns a new pointer of the same type.
func (x Pointer) New() any {
	return reflect.New(x.v.Type().Elem()).Interface()
}

// Set assigns the pointer value.
// v must be the same type of pointer.
func (x *Pointer) Set(v any) {
	x.v.Set(reflect.ValueOf(v))
}

// Unsafe returns the pointer itself.
func (x Pointer) Unsafe() uintptr {
	return x.v.Pointer()
}

// UnsafeSet sets the pointer.
func (x *Pointer) UnsafeSet(p unsafe.Pointer) {
	v := reflect.NewAt(x.v.Type().Elem(), p)
	x.v.Set(v)
}

// Value returns the pointer itself.
func (x Pointer) Value() any {
	return x.v.Interface()
}

// A Slice represents a generic slice or array.
// If the underlying value is an array, appending to it beyond its capacity will invalidate the Slice. Future method calls will do nothing.
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

func (x *Slice) Append(v ...any) {
	x.v.Set(reflect.AppendSlice(x.v, reflect.ValueOf(v)))
}

func (x *Slice) AppendPtr(v any) {
	x.v.Set(reflect.Append(x.v, reflect.ValueOf(v).Elem()))
}

func (x Slice) Cap() int {
	return x.v.Cap()
}

func (x Slice) Elem() reflect.Type {
	return x.elem
}

// Index returns the value at index i.
func (x Slice) Index(i int) any {
	return x.v.Index(i).Interface()
}

func (x Slice) Len() int {
	return x.v.Len()
}

// LenSet resizes the slice. If n would overflow capacity, a new array is allocated for the slice, discaring the previous data.
// LenSet should be used to prepare a slice of known length for future operations. For dynamic slice growth, use the Append methods.
func (x *Slice) LenSet(n int) {
	if n < x.Cap() {
		x.v.SetLen(n)
		return
	}

	// make new slice if insufficient capacity
	v := reflect.MakeSlice(x.v.Type(), n, n)
	x.v.Set(v)
}

// New returns a new pointer to the element type.
func (x Slice) New() any {
	return reflect.New(x.elem).Interface()
}

func (x *Slice) Set(i int, v any) {
	x.SetValue(i, reflect.ValueOf(v))
}

func (x *Slice) SetPtr(i int, v any) {
	x.SetValue(i, reflect.ValueOf(v).Elem())
}

func (x *Slice) SetValue(i int, v reflect.Value) {
	x.v.Index(i).Set(v)
}

// Unsafe returns a pointer to the first element of the slice.
func (x Slice) Unsafe() uintptr {
	return x.v.Index(0).UnsafeAddr()
}

// UnsafeSet points the slice to an arbitrary memory location.
func (x *Slice) UnsafeSet(p unsafe.Pointer, length int) {
	*(*reflect.SliceHeader)(unsafe.Pointer(x.v.UnsafeAddr())) = reflect.SliceHeader{
		Data: uintptr(p),
		Len:  length,
		Cap:  length,
	}
}

// A Struct represents a generic struct type.
type Struct struct {
	v reflect.Value
}

func makeStructVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(Struct{v})
}

func newStructVal(v reflect.Value) reflect.Value {
	return reflect.ValueOf(&Struct{v})
}

func (x Struct) Field(name string) any {
	return x.v.FieldByName(name).Interface()
}

func (x Struct) FieldNew(name string) any {
	sf, ok := x.v.Type().FieldByName(name)
	if !ok {
		panic("invalid field")
	}

	return reflect.New(sf.Type).Interface()
}

// FieldSet sets the value of the named field.
// Panics if the field is unexported.
func (x *Struct) FieldSet(field string, v any) {
	val := reflect.ValueOf(v)
	x.v.FieldByName(field).Set(val)
}

func (x *Struct) FieldSetPtr(field string, v any) {
	val := reflect.ValueOf(v).Elem()
	x.v.FieldByName(field).Set(val)
}

// Range returns an iterator over the struct's visible exported fields, excluding anonymous structs.
func (x Struct) Range() *StructIter {
	return newStructIter(x.v)
}

// RangeEx returns an iterator over all the struct's direct fields.
func (x Struct) RangeEx() *StructIterEx {
	return newStructIterEx(x.v)
}

func (x *Struct) Set(v any) {
	x.v.Set(reflect.ValueOf(v))
}

func (x *Struct) SetPtr(v any) {
	x.v.Set(reflect.ValueOf(v).Elem())
}

func (x Struct) Type() reflect.Type {
	return x.v.Type()
}

// Unsafe returns a pointer to the struct value.
// If the value is unaddressable, returns a pointer to a copy.
func (x Struct) Unsafe() uintptr {
	return unsafeAddr(x.v)
}

func (x *Struct) UnsafeSet(p unsafe.Pointer) {
	unsafeSet(x.v, p)
}

// A StructIter is used to iterate over a struct's exported fields.
// Anonymous structs will not be iterated directly, but their visible subfields will be.
// In essence, this is a "flattened" view of the struct.
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
			// otherwise nested visible fields will be double (or more) interated
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
// It returns true if it finds an exported, unobscured field, in which case the other methods become available.
func (x *StructIter) Next() bool {
	x.i++
	if x.i >= len(x.index) {
		return false
	}

	x.field = x.t.FieldByIndex(x.index[x.i])
	return true
}

// New returns a pointer to the field's type.
func (x StructIter) New() any {
	return reflect.New(x.field.Type).Interface()
}

// Set sets the value of the field.
// Panics if the struct is not addressable.
func (x StructIter) Set(v any) {
	val := reflect.ValueOf(v)
	x.v.FieldByIndex(x.index[x.i]).Set(val)
}

func (x StructIter) SetPtr(v any) {
	val := reflect.ValueOf(v).Elem()
	x.v.FieldByIndex(x.index[x.i]).Set(val)
}

func (x StructIter) Tag() StructTag {
	return x.field.Tag
}

func (x StructIter) Value() any {
	return x.v.FieldByIndex(x.index[x.i]).Interface()
}

// A StructIterEx is used to iterate over all of a struct's fields, exported or unexported.
// Unlike StructIter, it only operates on direct fields, not nested ones.
type StructIterEx struct {
	v         reflect.Value
	t         reflect.Type
	n         int // total field count
	i         int // current field index
	fieldInfo reflect.StructField
	field     reflect.Value // current field
}

func newStructIterEx(v reflect.Value) *StructIterEx {
	t := v.Type()

	// we need an addressable struct value in order to circumvent unexported field limitations
	// note: we could also hijack reflect.Value.flag and then continue to use reflect methods normally; but that seems a little excessive
	if !v.CanAddr() {
		p := reflect.New(t).Elem()
		p.Set(v)
		v = p
	}

	return &StructIterEx{
		v: v,
		t: t,
		n: t.NumField(),
		i: -1,
	}
}

// Next advances through the struct fields. It returns true if a new field has been loaded, making the other methods available for it.
func (x *StructIterEx) Next() bool {
	x.i++
	if x.i >= x.n {
		return false
	}

	x.field = x.v.Field(x.i)
	x.fieldInfo = x.t.Field(x.i)

	return true
}

func (x StructIterEx) New() any {
	return reflect.New(x.fieldInfo.Type).Interface()
}

func (x StructIterEx) Set(v any) {
	x.SetValue(reflect.ValueOf(v))
}

func (x StructIterEx) SetPtr(v any) {
	x.SetValue(reflect.ValueOf(v).Elem())
}

func (x StructIterEx) SetValue(v reflect.Value) {
	// use conventional method for exported fields
	if x.fieldInfo.PkgPath == "" {
		x.field.Set(v)
		return
	}

	fieldSet(x.field, v)
}

func (x StructIterEx) Tag() StructTag {
	return x.fieldInfo.Tag
}

func (x StructIterEx) Value() any {
	// use conventional method for exported fields
	if x.fieldInfo.PkgPath == "" {
		return x.field.Interface()
	}

	p := unsafe.Pointer(x.field.UnsafeAddr())
	v := reflect.NewAt(x.fieldInfo.Type, p).Elem()
	return v.Interface()
}

type StructTag = reflect.StructTag
