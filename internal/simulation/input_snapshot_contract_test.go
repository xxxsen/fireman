package simulation

import (
	"reflect"
	"testing"
)

// CanonicalJSON's stability rests on encoding/json emitting struct fields in
// declaration order and sorting map keys. String-keyed maps (Months/FXMonths)
// are therefore safe, but interface fields (dynamic type decides the output)
// and non-string map keys (custom TextMarshaler ordering pitfalls) are not.
// This contract test walks the full InputSnapshot type tree so any hash-unsafe
// field addition fails CI instead of silently breaking input_hash replay.
func TestInputSnapshotCanonicalJSONContract(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(tp reflect.Type, path string)
	walk = func(tp reflect.Type, path string) {
		//nolint:exhaustive // scalars fall through to default: they serialize deterministically
		switch tp.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Array:
			walk(tp.Elem(), path)
		case reflect.Map:
			if tp.Key().Kind() != reflect.String {
				t.Fatalf("map at %s (%s) has non-string keys; CanonicalJSON ordering is not guaranteed", path, tp)
			}
			walk(tp.Elem(), path+"[*]")
		case reflect.Struct:
			if seen[tp] {
				return
			}
			seen[tp] = true
			for i := 0; i < tp.NumField(); i++ {
				f := tp.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		case reflect.Interface:
			t.Fatalf("interface field at %s (%s): serialized form depends on the dynamic type", path, tp)
		default:
			// Scalars (bool/ints/floats/string) serialize deterministically.
		}
	}
	walk(reflect.TypeOf(InputSnapshot{}), "InputSnapshot")
}
