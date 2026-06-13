package jsonutil

import "reflect"

// NonNilSlices walks v in place and replaces nil slices with empty slices so JSON
// encoders emit [] instead of null.
func NonNilSlices(v any) {
	if v == nil {
		return
	}
	normalize(reflect.ValueOf(v))
}

func normalize(v reflect.Value) {
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Ptr:
		normalizePtr(v)
	case reflect.Struct:
		normalizeStruct(v)
	case reflect.Map:
		normalizeMapValueField(v)
	case reflect.Slice:
		normalizeSliceField(v)
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.String, reflect.UnsafePointer:
		return
	}
}

func normalizeMapValue(elem reflect.Value) reflect.Value {
	if !elem.IsValid() {
		return reflect.Value{}
	}
	cur := elem
	for cur.Kind() == reflect.Interface {
		if cur.IsNil() {
			return reflect.Value{}
		}
		inner := cur.Elem()
		if inner.Kind() == reflect.Slice && inner.IsNil() {
			empty := reflect.MakeSlice(inner.Type(), 0, 0)
			return reflect.ValueOf(empty.Interface())
		}
		if inner.Kind() == reflect.Map {
			normalize(inner)
			return elem
		}
		if inner.Kind() == reflect.Struct || inner.Kind() == reflect.Ptr {
			normalize(inner)
			return elem
		}
		cur = inner
	}
	if cur.Kind() == reflect.Slice && cur.IsNil() {
		empty := reflect.MakeSlice(cur.Type(), 0, 0)
		return reflect.ValueOf(empty.Interface())
	}
	normalize(cur)
	return reflect.Value{}
}
