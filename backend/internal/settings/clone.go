package settings

import "reflect"

// clone is a deep copy of s: every map, slice and pointer it holds is its
// own, so a caller changing what Load gave it changes nothing another
// caller holds, nor the settings parse keeps for the file's bytes.
func (s Settings) clone() Settings {
	var out Settings
	deepCopy(reflect.ValueOf(&out).Elem(), reflect.ValueOf(s))
	return out
}

// deepCopy sets dst, a settable value of src's type, to a copy of src
// sharing no map, slice or pointer with it. A nil map or slice stays nil
// and an empty one stays empty, as normal and Save tell the two apart.
func deepCopy(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Map:
		if src.IsNil() {
			return
		}
		t := src.Type()
		m := reflect.MakeMapWithSize(t, src.Len())
		// one key and value, set from each entry in turn: SetMapIndex
		// copies them into the map
		k, v := reflect.New(t.Key()).Elem(), reflect.New(t.Elem()).Elem()
		var kc, vc reflect.Value
		if !plain(t.Key()) {
			kc = reflect.New(t.Key()).Elem()
		}
		if !plain(t.Elem()) {
			vc = reflect.New(t.Elem()).Elem()
		}
		it := src.MapRange()
		for it.Next() {
			k.SetIterKey(it)
			v.SetIterValue(it)
			key, val := k, v
			if kc.IsValid() {
				kc.SetZero()
				deepCopy(kc, k)
				key = kc
			}
			if vc.IsValid() {
				vc.SetZero()
				deepCopy(vc, v)
				val = vc
			}
			m.SetMapIndex(key, val)
		}
		dst.Set(m)
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		n := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		if plain(src.Type().Elem()) {
			reflect.Copy(n, src)
		} else {
			for i := range src.Len() {
				deepCopy(n.Index(i), src.Index(i))
			}
		}
		dst.Set(n)
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		p := reflect.New(src.Type().Elem())
		deepCopy(p.Elem(), src.Elem())
		dst.Set(p)
	case reflect.Interface:
		if src.IsNil() {
			return
		}
		v := reflect.New(src.Elem().Type()).Elem()
		deepCopy(v, src.Elem())
		dst.Set(v)
	case reflect.Struct:
		if plain(src.Type()) {
			dst.Set(src)
			return
		}
		for i := range src.NumField() {
			deepCopy(dst.Field(i), src.Field(i))
		}
	default:
		dst.Set(src)
	}
}

// plain is whether a value of t holds nothing shared: no map, slice,
// pointer or interface anywhere in it, so a plain assignment copies it.
func plain(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return false
	case reflect.Array:
		return plain(t.Elem())
	case reflect.Struct:
		for i := range t.NumField() {
			if !plain(t.Field(i).Type) {
				return false
			}
		}
	}
	return true
}
