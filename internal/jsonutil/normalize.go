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
		if v.IsNil() {
			return
		}
		normalize(v.Elem())
	case reflect.Struct:
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
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}
		for _, key := range v.MapKeys() {
			elem := v.MapIndex(key)
			if next := normalizeMapValue(elem); next.IsValid() {
				v.SetMapIndex(key, next)
			}
		}
	case reflect.Slice:
		if v.CanSet() && v.IsNil() {
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			return
		}
		for i := 0; i < v.Len(); i++ {
			normalize(v.Index(i))
		}
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
