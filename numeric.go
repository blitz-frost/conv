package conv

import (
	"reflect"

	"github.com/blitz-frost/conv/numeric"
)

// numericChart inspects x.basic for available numeric kinds and returns a map of additional numeric kinds that could be extrapolated from it.
// For a returned key-value pair:
//
// key -> numeric kind that can be extrapolated
//
// value -> existing kind that the key can be extrapolated from
func (x Scheme) numericChart() map[reflect.Kind]reflect.Kind {
	if len(x.basic) == 0 {
		return nil
	}

	have := make(map[reflect.Kind]struct{})
	miss := make(map[reflect.Kind]struct{})

	for k, _ := range numeric.Descriptors {
		if _, ok := x.basic[k]; ok {
			have[k] = struct{}{}
		} else {
			miss[k] = struct{}{}
		}
	}

	o := make(map[reflect.Kind]reflect.Kind)

	for k, _ := range miss {
		r := -1
		for kk, _ := range have {
			rate := numeric.RateConversion(kk, k)
			if rate > -1 && ((r == -1) || rate < r) {
				r = rate
				o[k] = kk
			}
		}
	}

	return o

	/*
		// known will store already known extrapolations, including invalid ones
		// separate from o beacuse we only want to return the valid ones
		known := make(map[reflect.Kind]reflect.Kind)

		for k, _ := range numeric.Descriptors {
			// skip already present kinds
			if _, ok := x.basic[k]; ok {
				continue
			}

			x.numericChartRec(k, known, o)
		}

		return o
	*/
}

func (x Scheme) fillNumeric() {
	extra := x.numericChart()
	if len(extra) == 0 {
		return
	}

	conv := reflect.ValueOf(numeric.Convert)
	for k, ex := range extra {
		exFunc := x.basic[ex]
		exType := numeric.Types[ex]
		t := reflect.FuncOf([]reflect.Type{x.dstPtrType, numeric.Types[k]}, []reflect.Type{errorType}, false)
		v := reflect.MakeFunc(t, func(args []reflect.Value) []reflect.Value {
			arg := reflect.New(exType)
			conv.Call([]reflect.Value{arg, args[1]})
			return exFunc.Call([]reflect.Value{args[0], arg.Elem()})
		})

		x.basic[k] = v
	}
}
