package jsonutil

import "reflect"

func normalizePtr(v reflect.Value) {
	if v.IsNil() {
		return
	}
	normalize(v.Elem())
}

func normalizeStruct(v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.CanSet() {
			if f.Kind() == reflect.Slice && f.IsNil() {
				f.Set(reflect.MakeSlice(f.Type(), 0, 0))
				continue
			}
			normalize(f)
			continue
		}
		if f.CanAddr() {
			normalize(f.Addr())
		} else {
			normalize(f)
		}
	}
}

func normalizeMapValueField(v reflect.Value) {
	if v.Type().Key().Kind() != reflect.String {
		return
	}
	for _, key := range v.MapKeys() {
		elem := v.MapIndex(key)
		if next := normalizeMapValue(elem); next.IsValid() {
			v.SetMapIndex(key, next)
		}
	}
}

func normalizeSliceField(v reflect.Value) {
	if v.CanSet() && v.IsNil() {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return
	}
	for i := 0; i < v.Len(); i++ {
		normalize(v.Index(i))
	}
}
