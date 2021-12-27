package conv

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/blitz-frost/conv/numeric"
)

var (
	emptyType  = reflect.TypeOf(new(interface{})).Elem()
	errorType  = reflect.TypeOf(new(error)).Elem()
	mapType    = reflect.TypeOf(Map{})
	sliceType  = reflect.TypeOf(Slice{})
	structType = reflect.TypeOf(Struct{})

	simpleTypes = make(map[reflect.Kind]reflect.Type)
)

func init() {
	for k, t := range numeric.Types {
		simpleTypes[k] = t
	}
	simpleTypes[reflect.Bool] = reflect.TypeOf(false)
	simpleTypes[reflect.String] = reflect.TypeOf("")
}

// convertGeneric returns v as value usable by a generic conversion.
func convertGeneric(k reflect.Kind, v reflect.Value) reflect.Value {
	if t, ok := simpleTypes[k]; ok {
		return v.Convert(t)
	}

	switch k {
	case reflect.Array:
		fallthrough
	case reflect.Slice:
		return reflect.ValueOf(Slice{v})
	case reflect.Map:
		return reflect.ValueOf(Map{v})
	case reflect.Struct:
		return reflect.ValueOf(Struct{v})
	}

	return v
}

// funcVal returns a function value from v.
// v must be a function or point to one.
func funcVal(v interface{}) (reflect.Value, error) {
	if v == nil {
		return reflect.Value{}, errors.New("nil input")
	}

	o := reflect.Indirect(reflect.ValueOf(v))
	if o.Kind() != reflect.Func {
		return reflect.Value{}, errors.New("non-function input")
	}

	return o, nil
}

// funcRefVal is like valFunc, but also checks that the function value be settable.
func funcRefVal(v interface{}) (reflect.Value, error) {
	o, err := funcVal(v)
	if err != nil {
		return o, err
	}

	if !o.CanSet() {
		return reflect.Value{}, errors.New("non-settable input")
	}

	return o, nil
}

type funcEvalSide int

func (x funcEvalSide) String() string {
	switch x {
	case funcEvalIn:
		return "input"
	case funcEvalOut:
		return "output"
	}
	return ""
}

const (
	funcEvalIn funcEvalSide = iota
	funcEvalOut
)

// A funcEval is used to examine function values, ensuring they conform to certain requirements.
// in and out should hold desired input and output types. A nil element is interpreted as any type being valid.
type funcEval struct {
	in  []reflect.Type
	out []reflect.Type
}

// makeFuncEval returns a funcEval for a conversion function to v's type.
func makeFuncEval(v interface{}) funcEval {
	return funcEval{
		in:  []reflect.Type{reflect.PtrTo(reflect.TypeOf(v)), nil},
		out: []reflect.Type{errorType},
	}
}

func (x funcEval) checkType(t reflect.Type, side funcEvalSide) error {
	var (
		want []reflect.Type
		get  func(int) reflect.Type
		num  int
	)
	switch side {
	case funcEvalIn:
		want = x.in
		get = t.In
		num = t.NumIn()
	case funcEvalOut:
		want = x.out
		get = t.Out
		num = t.NumOut()
	}

	if want == nil {
		return nil
	}

	if num != len(want) {
		return fmt.Errorf("expected function with %v %vs", num, side)
	}

	for i, v := range want {
		if v == nil {
			continue
		}
		if get(i) != v {
			return fmt.Errorf("expected %v #%v to be %v", side, i, v)
		}
	}

	return nil
}

func (x funcEval) eval(v interface{}) (reflect.Value, reflect.Type, error) {
	return x.evalWith(v, funcVal)
}

func (x funcEval) evalRef(v interface{}) (reflect.Value, reflect.Type, error) {
	return x.evalWith(v, funcRefVal)
}

func (x funcEval) evalValue(v reflect.Value) (reflect.Type, error) {
	t := v.Type()

	if err := x.checkType(t, funcEvalIn); err != nil {
		return nil, err
	}
	if err := x.checkType(t, funcEvalOut); err != nil {
		return nil, err
	}

	return t, nil
}

func (x funcEval) evalWith(v interface{}, check func(interface{}) (reflect.Value, error)) (reflect.Value, reflect.Type, error) {
	oval, err := check(v)
	if err != nil {
		return reflect.Value{}, nil, err
	}

	otyp, err := x.evalValue(oval)
	if err != nil {
		return reflect.Value{}, nil, err
	}

	return oval, otyp, nil
}

type Scheme struct {
	basic    map[reflect.Kind]reflect.Value
	specific map[reflect.Type]reflect.Value

	rec reflect.Value // recursion reference

	dstPtrType reflect.Type
	evaluator  funcEval // used to check that functions conform to expected signature
}

// returns an empty Scheme to convert to v's type.
// Panics if v is nil.
func MakeScheme(v interface{}) Scheme {
	return Scheme{
		basic:      make(map[reflect.Kind]reflect.Value),
		specific:   make(map[reflect.Type]reflect.Value),
		dstPtrType: reflect.PtrTo(reflect.TypeOf(v)),
		evaluator:  makeFuncEval(v),
	}
}

func (x Scheme) fill() {
	if len(x.basic) == 0 {
		return
	}

	x.fillNumeric()
}

// Build packages all currently loaded conversion functions into a single function.
// fn must be a pointer to a function with signature:
//
// func(*dstType, interface{}) error
//
// where dstType is the same as the one in loaded functions.
//
// On successful build, fn can be used to convert to the destination type.
// It will flatten pointer inputs.
//
// If a basic numeric type conversion is defined, all other basic numeric types that can be extrapolated losslesly from them are automatically filled in.
// Each kind will prefer the closest (in size) available conversion of the same kind, followed by the following preferences (">" = "prefered over"):
//
// float -> complex
//
// int -> float > complex
//
// uint -> int > float > complex
//
// Finally, if no explicit (specific or generic) conversion has been defined for a particular dstType-srcType pair, but srcType can be directly converted to dstType, as per the Go specification, the direct conversion is used.
// Numeric types will use the rules described above, instead of the Go rules.
func (x Scheme) Build(fn interface{}) error {
	if len(x.basic) == 0 && len(x.specific) == 0 {
		return errors.New("empty scheme")
	}

	x.evaluator.in[1] = emptyType // we explicitly want interface{} input for the packaged function
	defer func() {
		x.evaluator.in[1] = nil
	}()

	v, t, err := x.evaluator.evalRef(fn)
	if err != nil {
		return err
	}

	x.fill()

	numConv := reflect.ValueOf(numeric.Convert)
	dstType := x.dstPtrType.Elem()
	dstKind := dstType.Kind()
	_, dstIsNum := numeric.Types[dstKind]

	f := reflect.MakeFunc(t, func(args []reflect.Value) []reflect.Value {
		src := reflect.Indirect(args[1].Elem()) // arg is seen as interface{} by f
		args[1] = src
		if specific, ok := x.specific[src.Type()]; ok {
			return specific.Call(args)
		}

		k := src.Kind()
		if basic, ok := x.basic[k]; ok {
			args[1] = convertGeneric(k, args[1])
			return basic.Call(args)
		}

		// try direct conversion
		_, srcIsNum := numeric.Types[k]
		if dstIsNum && srcIsNum {
			if numeric.RateConversion(dstKind, k) > -1 { //TODO this is a double check if numeric conversions have been explicitly defined
				return numConv.Call(args)
			}
		} else if src.CanConvert(dstType) {
			args[0].Elem().Set(args[1].Convert(dstType))
			return []reflect.Value{reflect.Zero(errorType)}
		}

		err := reflect.ValueOf(errors.New("invalid conversion"))
		return []reflect.Value{err}
	})

	v.Set(f)
	if x.rec.IsValid() {
		x.rec.Set(f)
	}
	return nil
}

// Load registers a function to be used for conversion to a destination type.
// fn must be a function with signature:
//
// func(*dstType, srcType) error
//
// If srcType is a basic type, fn will serve as a generic conversion function for all values of the same kind.
// int and uint are treated as equivalent to their respective basic types (int32/int64 and uint32/uint64). Loading one of these types will overwrite its equivalent.
//
// If srcType is a defined type, fn will only be used for that specific type.
// A few special srcTypes are recognized as follows:
//
// Map -> generic map conversion
// Slice -> generic array or slice conversion
// Struct -> generic struct conversion
//
// Multiple functions may be loaded for the same source type, overwriting the previous one.
func (x *Scheme) Load(fn interface{}) error {
	v, t, err := x.evaluator.eval(fn)
	if err != nil {
		return err
	}

	in := t.In(1)
	switch in {
	case sliceType:
		x.basic[reflect.Array] = v
		x.basic[reflect.Slice] = v
	case mapType:
		x.basic[reflect.Map] = v
	case structType:
		x.basic[reflect.Struct] = v
	default:
		if in.PkgPath() == "" && in.Name() != "" { // type.Name() returns the empty string for non defined composite types
			k := in.Kind()
			x.basic[k] = v

			// int and uint exclusions
			if kk, ok := numericExclusions[k]; ok {
				delete(x.basic, kk)
			}
		} else {
			x.specific[in] = v
		}
	}

	return nil
}

var numericExclusions = make(map[reflect.Kind]reflect.Kind)

func init() {
	var intX, uintX reflect.Kind
	switch numeric.Arch() {
	case 64:
		intX = reflect.Int64
		uintX = reflect.Uint64
	case 32:
		intX = reflect.Int32
		uintX = reflect.Uint32
	}

	numericExclusions[reflect.Int] = intX
	numericExclusions[reflect.Uint] = uintX
	numericExclusions[intX] = reflect.Int
	numericExclusions[uintX] = reflect.Uint
}

// Recursion takes a pointer to a function with signature:
//
// func(*dstType, interface{}) error
//
// That function may then be used as a standin for the final conversion function that will be built from this Scheme.
// This allows definition of recursive conversions.
func (x *Scheme) Recursion(fn interface{}) error {
	v, _, err := x.evaluator.evalRef(fn)
	if err != nil {
		return err
	}

	x.rec = v

	return nil
}

type Map struct {
	v reflect.Value
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

type MapIter struct {
	v *reflect.MapIter
}

func (x MapIter) Key() interface{} {
	return x.v.Key().Interface()
}

func (x MapIter) Next() bool {
	return x.v.Next()
}

func (x MapIter) Value() interface{} {
	return x.v.Value().Interface()
}

type Slice struct {
	v reflect.Value // underlying value
}

func (x Slice) Index(i int) interface{} {
	return x.v.Index(i).Interface()
}

func (x Slice) Len() int {
	return x.v.Len()
}

// A Struct allows access only to exported fields.
// Embedded structs are flattened and become invisible. Fields of embedded structs with conflicting names will be invisible.
type Struct struct {
	v reflect.Value
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

func (x StructIter) Value() interface{} {
	return x.v.FieldByIndex(x.index[x.i]).Interface()
}

func (x StructIter) Name() string {
	return x.field.Name
}

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

type StructTag = reflect.StructTag
