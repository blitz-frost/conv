// Package conv defines a framework for Go value conversions. It does not strictly follow the Go specification.
//
// All bool, string and numeric types are "simple types".
// All pointer, array, slice, struct, map, channel and function types are "composite types".
//
// A type is a "base type" if it is either a built-in simple type, or an undefined composite type of only built-in simple types.
//
// "A -> B" will be used to denote that type A is directly convertible to type B.
// "A <-> B" will be used to denote that types A and B are directly convertible between themselves.
//
// Implicit direct conversions
//
// All types are directly convertible to other types that have the same underlying memory structure.
// All simple types are directly convertible to those with the same underlying built-in simple type.
// All composite types are directly convertible to those with the same equivalent underlying structure.
// A conversion that relies on this in-memory equivalence is a "basic conversion".
//
// Slice/array types are convertible to array/slice types, if the source element type is convertible to the destination element type. TODO
//
// String types are mutually convertible to byte slice/array types. Arrays involved in implicit conversions must have the appropriate size for their conversion counterpart, otherwise these conversions panic. TODO
//
// Finally, all numeric types are directly convertible to other numeric types that can hold the same information losslessly. That is to say: uint -> int -> float -> complex (of appropriate size).
package conv

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/blitz-frost/conv/numeric"
)

// An Inverse is used to package conversions from a single source type to various destination types.
type Inverse struct {
	// conversion functions
	cache    map[reflect.Type]implementation // cache types evaluated at conversion time for rapid subsequent resolution; this is mostly significant for basic conversions
	specific map[reflect.Type]implementation // specific to certain types
	generic  map[reflect.Kind]implementation // using generics
	basic    map[uint64]implementation       // for all types with the same memory structure
	numeric  map[reflect.Kind]implementation // basic numeric types; subset of basic

	srcType   reflect.Type
	evaluator funcEval // used to check that functions conform to expected signature
}

// MakeInverse returns an empty Inverse scheme to convert from v's type.
// Panics if v is nil.
func MakeInverse(v interface{}) Inverse {
	return Inverse{
		cache:     make(map[reflect.Type]implementation),
		specific:  make(map[reflect.Type]implementation),
		generic:   make(map[reflect.Kind]implementation),
		basic:     make(map[uint64]implementation),
		numeric:   make(map[reflect.Kind]implementation),
		srcType:   reflect.TypeOf(v),
		evaluator: makeFuncEvalInverse(v),
	}
}

// Build packages all currently loaded conversion functions into a single function.
// fn must be a pointer to a function with signature:
//
// func(interface{}, srcType) error
//
// where srcType is the same as the one in loaded functions.
//
// On successful build, fn can be used to convert from the source type.
//
// If a basic numeric type conversion is defined, all other basic numeric types that can be extrapolated losslesly from them are automatically filled in.
//
// For a given dstType, if multiple conversions are available, the order of preference is: specific > generic > basic > closest numeric substitute.
//
// Finally, if no explicit conversion has been defined for a particular dstType-srcType pair, but srcType can be implicitly converted (as defined by this package) to dstType, the implicit conversion is used.
func (x Inverse) Build(fn interface{}) error {
	x.evaluator.in[0] = emptyType // we explicitly want interface{} input for the packaged function
	defer func() {
		x.evaluator.in[0] = pointerType
	}()

	v, t, err := x.evaluator.evalRef(fn)
	if err != nil {
		return err
	}

	x.fill()

	if len(x.specific) == 0 && len(x.generic) == 0 && len(x.basic) == 0 {
		return errors.New("empty inverse")
	}

	var ff func([]reflect.Value) []reflect.Value
	ff = func(args []reflect.Value) []reflect.Value {
		dstPtr := args[0].Elem() // arg is seen as interface{} by f
		dst := dstPtr.Elem()
		dstType := dst.Type()
		args[0] = dstPtr

		if cache, ok := x.cache[dstType]; ok {
			return cache(args)
		}

		if specific, ok := x.specific[dstType]; ok {
			x.cache[dstType] = specific
			return specific(args)
		}

		k := dst.Kind()
		kGen := k
		if _, ok := numeric.Types[kGen]; ok {
			kGen = reflectNumeric
		}
		if generic, ok := x.generic[kGen]; ok {
			x.cache[dstType] = generic
			return generic(args)
		}

		h := baseOf(dstType).hash()
		if basic, ok := x.basic[h]; ok {
			x.cache[dstType] = basic
			return basic(args)
		}

		if numType, dstIsNum := numeric.Types[k]; dstIsNum {
			var (
				best       implementation
				bestRating = -1
				bestKind   reflect.Kind
			)
			for kk, fn := range x.numeric {
				r := numeric.RateConversion(k, kk)
				if r >= 0 && (r < bestRating || bestRating == -1) {
					bestKind = kk
					bestRating = r
					best = fn
				}
			}

			if bestRating > -1 {
				bestType := numeric.Types[bestKind]
				numPtrType := reflect.PtrTo(bestType)
				funcType := reflect.FuncOf([]reflect.Type{numPtrType, x.srcType}, []reflect.Type{errorType}, false)
				convFunc := reflect.MakeFunc(funcType, func(args []reflect.Value) []reflect.Value {
					bestPtr := reflect.New(bestType)
					dstPtr := args[0]
					args[0] = bestPtr
					err := best(args)

					args[1] = bestPtr.Elem()
					args[0] = dstPtr
					convNumeric(args)

					return err
				})
				x.loadBasic(numType, convFunc, false)
				basic := x.basic[h]
				x.cache[dstType] = basic
				return basic(args)
			} else {
				x.basic[h] = convInvalid
			}
		}

		x.cache[dstType] = convInvalid
		return convInvalid(args)
	}

	f := reflect.MakeFunc(t, ff)

	v.Set(f)
	return nil
}

// Load registers a function to be used for conversion from a source type.
// fn must be a function with signature:
//
// func(*dstType, srcType) error
//
// If dstType is an undefined simple type, or an undefined composed type of only undefined types, fn will serve as conversion function for all equivalent types.
// int and uint are treated as equivalent to their respective aliases (int32/int64 and uint32/uint64). Loading one of these types will overwrite its alias.
//
// If dstType is a defined type, fn will only be used for that specific type.
// A few special dstTypes are recognized as follows:
//
// Array -> generic array conversion
//
// Map -> generic map conversion
//
// Number -> generic numeric conversion
//
// Pointer -> generic pointer conversion
//
// Slice -> generic slice conversion
//
// Struct -> generic struct conversion
//
// Multiple functions may be loaded for the same destination type, overwriting the previous one.
func (x Inverse) Load(fn interface{}) error {
	v, t, err := x.evaluator.eval(fn)
	if err != nil {
		return err
	}

	out := t.In(0).Elem()

	if isBasic(out) {
		x.loadBasic(out, v, true)
		return nil
	}

	switch out {
	case arrayType:
		x.loadGeneric(reflect.Array, v, newArrayVal)
	case numericType:
		x.loadGeneric(reflectNumeric, v, newNumberVal)
	case sliceType:
		x.loadGeneric(reflect.Slice, v, newSliceVal)
	case mapType:
		x.loadGeneric(reflect.Map, v, newMapVal)
	case pointerType:
		x.loadGeneric(reflect.Ptr, v, newPointerVal)
	case structType:
		x.loadGeneric(reflect.Struct, v, newStructVal)
	default:
		x.loadSpecific(out, v)
	}

	return nil
}

func (x Inverse) fill() {
	srcBase := baseOf(x.srcType)
	if !srcBase.isConcrete() {
		return
	}

	h := srcBase.hash()
	if _, ok := x.basic[h]; ok {
		return
	}

	t := srcBase.asType()
	funcType := reflect.FuncOf([]reflect.Type{reflect.PtrTo(t), x.srcType}, []reflect.Type{errorType}, false)
	funcVal := reflect.MakeFunc(funcType, convBasic)

	x.loadBasic(t, funcVal, true)
}

func (x Inverse) loadBasic(t reflect.Type, convFunc reflect.Value, original bool) {
	x.loadSpecific(t, convFunc)

	fn := func(args []reflect.Value) []reflect.Value {
		// call actual conversion
		dstPtr := args[0]
		args[0] = reflect.New(t)
		err := convFunc.Call(args)

		// convert to actual output
		args[1] = args[0].Elem()
		args[0] = dstPtr
		convBasic(args)

		return err
	}

	h := baseOf(t).hash()
	x.basic[h] = fn

	if original {
		k := t.Kind()
		if _, ok := numeric.Types[k]; ok {
			x.numeric[k] = fn
		}
	}
}

func (x Inverse) loadGeneric(k reflect.Kind, convFunc reflect.Value, maker func(reflect.Value) reflect.Value) {
	x.generic[k] = func(args []reflect.Value) []reflect.Value {
		args[0] = maker(args[0].Elem())
		return convFunc.Call(args)
	}
}

func (x Inverse) loadSpecific(t reflect.Type, convFunc reflect.Value) {
	x.specific[t] = func(args []reflect.Value) []reflect.Value {
		return convFunc.Call(args)
	}
}

// A Scheme is used to package conversions from various source types to a single destination type.
type Scheme struct {
	// conversion functions
	cache    map[reflect.Type]implementation // cache types evaluated at conversion time for rapid subsequent resolution; this is mostly significant for basic conversions
	specific map[reflect.Type]implementation // specific to certain types
	generic  map[reflect.Kind]implementation // using generics
	basic    map[uint64]implementation       // for all types with the same memory structure
	numeric  map[reflect.Kind]implementation // basic numeric types; subset of basic

	dstType    reflect.Type
	dstPtrType reflect.Type
	evaluator  funcEval // used to check that functions conform to expected signature
}

// MakeScheme returns an empty Scheme to convert to v's type.
// Panics if v is nil.
func MakeScheme(v interface{}) Scheme {
	dstType := reflect.TypeOf(v)
	return Scheme{
		cache:      make(map[reflect.Type]implementation),
		specific:   make(map[reflect.Type]implementation),
		generic:    make(map[reflect.Kind]implementation),
		basic:      make(map[uint64]implementation),
		numeric:    make(map[reflect.Kind]implementation),
		dstType:    dstType,
		dstPtrType: reflect.PtrTo(dstType),
		evaluator:  makeFuncEvalScheme(v),
	}
}

// Build packages all currently loaded conversion functions into a single function.
// fn must be a pointer to a function with signature:
//
// func(*dstType, interface{}) error
//
// where dstType is the same as the one in loaded functions.
//
// On successful build, fn can be used to convert to the destination type.
//
// If a basic numeric type conversion is defined, all other basic numeric types that can be extrapolated losslesly from them are automatically filled in.
//
// For a given srcType, if multiple conversions are available, the order of preference is: specific > generic > basic > closest numeric substitute.
//
// Finally, if no explicit conversion has been defined for a particular dstType-srcType pair, but srcType can be implicitly converted (as defined by this package) to dstType, the implicit conversion is used.
func (x Scheme) Build(fn interface{}) error {
	x.evaluator.in[1] = emptyType // we explicitly want interface{} input for the packaged function
	defer func() {
		x.evaluator.in[1] = nil
	}()

	v, t, err := x.evaluator.evalRef(fn)
	if err != nil {
		return err
	}

	x.fill()

	// if we end up with no implicit or explicit conversions, we might as well abort
	// numeric is a subset of basic; no need to check it
	if len(x.specific) == 0 && len(x.generic) == 0 && len(x.basic) == 0 {
		return errors.New("empty scheme")
	}

	var ff func([]reflect.Value) []reflect.Value
	ff = func(args []reflect.Value) []reflect.Value {
		src := args[1].Elem() // arg is seen as interface{} by f
		args[1] = src
		srcType := src.Type()

		// check cache first
		if cache, ok := x.cache[srcType]; ok {
			return cache(args)
		}

		// check specific conversions
		if specific, ok := x.specific[srcType]; ok {
			x.cache[srcType] = specific
			return specific(args)
		}

		// check generic conversions
		k := src.Kind()
		kGen := k
		if _, ok := numeric.Types[kGen]; ok {
			kGen = reflectNumeric
		}
		if generic, ok := x.generic[kGen]; ok {
			x.cache[srcType] = generic
			return generic(args)
		}

		// check basic conversions
		h := baseOf(srcType).hash()
		if basic, ok := x.basic[h]; ok {
			x.cache[srcType] = basic
			return basic(args)
		}

		// check numeric substitution
		if numType, srcIsNum := numeric.Types[k]; srcIsNum {
			// find the best available substitute
			var (
				best       implementation
				bestRating = -1
				bestKind   reflect.Kind
			)
			for kk, fn := range x.numeric {
				r := numeric.RateConversion(kk, k)
				if r >= 0 && (r < bestRating || bestRating == -1) {
					bestKind = kk
					bestRating = r
					best = fn
				}
			}

			if bestRating > -1 {
				bestType := numeric.Types[bestKind]
				funcType := reflect.FuncOf([]reflect.Type{x.dstPtrType, numType}, []reflect.Type{errorType}, false)
				convFunc := reflect.MakeFunc(funcType, func(args []reflect.Value) []reflect.Value {
					bestPtr := reflect.New(bestType)
					dst := args[0]
					args[0] = bestPtr
					convNumeric(args)

					args[1] = bestPtr.Elem()
					args[0] = dst
					return best(args)
				})
				x.loadBasic(numType, convFunc, false)
				basic := x.basic[h]
				x.cache[srcType] = basic
				return basic(args)
			} else {
				x.basic[h] = convInvalid
			}
		}

		x.cache[srcType] = convInvalid
		return convInvalid(args)
	}

	f := reflect.MakeFunc(t, ff)

	v.Set(f)
	return nil
}

// Load registers a function to be used for conversion to a destination type.
// fn must be a function with signature:
//
// func(*dstType, srcType) error
//
// If srcType is an undefined simple type, or an undefined composed type of only undefined types, fn will serve as conversion function for all equivalent types.
// int and uint are treated as equivalent to their respective aliases (int32/int64 and uint32/uint64). Loading one of these types will overwrite its alias.
//
// Otherwise, fn will only be used for that specific type.
// A few special srcTypes are recognized as follows:
//
// Array -> generic array conversion
//
// Map -> generic map conversion
//
// Number -> generic numeric conversion
//
// Pointer -> generic pointer conversion
//
// Slice -> generic slice conversion
//
// Struct -> generic struct conversion
//
// Multiple functions may be loaded for the same source type, overwriting the previous one.
func (x Scheme) Load(fn interface{}) error {
	v, t, err := x.evaluator.eval(fn)
	if err != nil {
		return err
	}

	in := t.In(1)

	if isBasic(in) {
		x.loadBasic(in, v, true)
		return nil
	}

	switch in {
	case arrayType:
		x.loadGeneric(reflect.Array, v, makeArrayVal)
	case numericType:
		x.loadGeneric(reflectNumeric, v, makeNumberVal)
	case sliceType:
		x.loadGeneric(reflect.Slice, v, makeSliceVal)
	case mapType:
		x.loadGeneric(reflect.Map, v, makeMapVal)
	case pointerType:
		x.loadGeneric(reflect.Ptr, v, makePointerVal)
	case structType:
		x.loadGeneric(reflect.Struct, v, makeStructVal)
	default:
		x.loadSpecific(in, v)
	}

	return nil
}

// fill generates the implicit conversion if a suitable basic conversion has not been defined.
func (x Scheme) fill() {
	dstBase := baseOf(x.dstType)
	if !dstBase.isConcrete() {
		return // cannot implicitly convert if interfaces are involved
	}

	h := dstBase.hash()
	if _, ok := x.basic[h]; ok {
		return // already have explicit basic conversion
	}

	t := dstBase.asType()
	funcType := reflect.FuncOf([]reflect.Type{x.dstPtrType, t}, []reflect.Type{errorType}, false)
	funcVal := reflect.MakeFunc(funcType, convBasic)

	x.loadBasic(t, funcVal, true)
}

// if original is true, will remember the conversion as an original conversion for the purpose of numeric conversions. This is to avoid indirect conversion chains.
func (x Scheme) loadBasic(t reflect.Type, convFunc reflect.Value, original bool) {
	x.loadSpecific(t, convFunc) // bypass intermediary conversion for the basic type itself

	fn := func(args []reflect.Value) []reflect.Value {
		// convert to basic type
		srcPtr := reflect.New(t)
		dstPtr := args[0]
		args[0] = srcPtr
		convBasic(args)

		// call actual conversion
		args[1] = srcPtr.Elem()
		args[0] = dstPtr
		return convFunc.Call(args)
	}

	h := baseOf(t).hash()
	x.basic[h] = fn

	if original {
		k := t.Kind()
		if _, ok := numeric.Types[k]; ok {
			x.numeric[k] = fn
		}
	}
}

func (x Scheme) loadGeneric(k reflect.Kind, convFunc reflect.Value, maker func(reflect.Value) reflect.Value) {
	x.generic[k] = func(args []reflect.Value) []reflect.Value {
		args[1] = maker(args[1])
		return convFunc.Call(args)
	}
}

func (x Scheme) loadSpecific(t reflect.Type, convFunc reflect.Value) {
	x.specific[t] = func(args []reflect.Value) []reflect.Value {
		return convFunc.Call(args)
	}
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
// in and out should hold desired input and output types.
// The generic types defined in this package may be used as stand-ins for any type of those respective kinds.
// A nil element is interpreted as any type being valid.
type funcEval struct {
	in  []reflect.Type
	out []reflect.Type
}

// makeFuncEvalInverse returns a funcEval for a conversion function from v's type.
func makeFuncEvalInverse(v interface{}) funcEval {
	return funcEval{
		in:  []reflect.Type{pointerType, reflect.TypeOf(v)},
		out: []reflect.Type{errorType},
	}
}

// makeFuncEvalScheme returns a funcEval for a conversion function to v's type.
func makeFuncEvalScheme(v interface{}) funcEval {
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
		var k reflect.Kind

		switch v {
		case nil:
			continue
		case mapType:
			k = reflect.Map
		case pointerType:
			k = reflect.Ptr
		case sliceType:
			k = reflect.Slice
		case structType:
			k = reflect.Struct
		}

		typ := get(i)
		if k == reflect.Invalid {
			if typ != v {
				return fmt.Errorf("expected %v #%v to be %v", side, i, v)
			}
		} else {
			if typ.Kind() != k {
				return fmt.Errorf("expected %v #%v to be %v type", side, i, k)
			}
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

var (
	arrayType   = reflect.TypeOf(Array{})
	emptyType   = reflect.TypeOf(new(interface{})).Elem()
	errorType   = reflect.TypeOf(new(error)).Elem()
	mapType     = reflect.TypeOf(Map{})
	numericType = reflect.TypeOf(Number{})
	pointerType = reflect.TypeOf(Pointer{})
	sliceType   = reflect.TypeOf(Slice{})
	structType  = reflect.TypeOf(Struct{})

	simpleTypes = make(map[reflect.Kind]reflect.Type) // types corresponding to simple kinds (int, bool etc.)
)

func init() {
	for k, t := range numeric.Types {
		simpleTypes[k] = t
	}
	simpleTypes[reflect.Bool] = reflect.TypeOf(false)
	simpleTypes[reflect.String] = reflect.TypeOf("")
	simpleTypes[reflect.UnsafePointer] = reflect.TypeOf(new(unsafe.Pointer)).Elem()
}

// convBasic converts args[1] to args[0] through direct pointer reinterpretation
func convBasic(args []reflect.Value) []reflect.Value {
	// we need to make src addressable
	if !args[1].CanAddr() {
		srcPtr := reflect.New(args[1].Type())
		srcTmp := srcPtr.Elem()
		srcTmp.Set(args[1])
		args[1] = srcTmp
	}

	// reinterpret src
	dst := args[0].Elem()
	dstPtr := reflect.NewAt(dst.Type(), unsafe.Pointer(args[1].UnsafeAddr()))
	dst.Set(dstPtr.Elem())

	return []reflect.Value{errValNone}
}

// convInvalid returns the invalid conversion error
func convInvalid([]reflect.Value) []reflect.Value {
	return []reflect.Value{errValInvalid}
}

// convNumeric wraps numeric.Convert
func convNumeric(args []reflect.Value) []reflect.Value {
	return numericFunc.Call(args)
}

type implementation = func([]reflect.Value) []reflect.Value

var numericFunc = reflect.ValueOf(numeric.Convert)

var (
	ErrInvalid    = errors.New("invalid conversion")
	errValInvalid = reflect.ValueOf(ErrInvalid)
	errValNone    = reflect.Zero(errorType)
)
