package conv

import (
	"reflect"

	"github.com/blitz-frost/conv/numeric"
)

func IsNumeric(k reflect.Kind) bool {
	if _, ok := numeric.Types[k]; ok {
		return true
	}
	return false
}
