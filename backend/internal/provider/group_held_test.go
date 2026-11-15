package provider

import (
	"fmt"

	"reflect"
)

// fillEvery gives every field of v a value, and each slice, map and pointer one
// element.
func fillEvery(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fillEvery(v.Field(i))
			}
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillEvery(v.Index(0))
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fillEvery(k)
		fillEvery(e)
		v.SetMapIndex(k, e)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillEvery(v.Elem())
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64, reflect.Int32:
		v.SetInt(1)
	case reflect.Float64:
		v.SetFloat(1)
	}
}

// sharedRef names the first slice, map or pointer a and b hold in common.
func sharedRef(a, b reflect.Value, path string) string {
	switch a.Kind() {
	case reflect.Struct:
		for i := range a.NumField() {
			if a.Type().Field(i).IsExported() {
				if p := sharedRef(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); p != "" {
					return p
				}
			}
		}
	case reflect.Slice:
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return path
		}
		for i := range a.Len() {
			if p := sharedRef(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i)); p != "" {
				return p
			}
		}
	case reflect.Map, reflect.Pointer:
		if !a.IsNil() && a.Pointer() == b.Pointer() {
			return path
		}
		if a.Kind() == reflect.Pointer && !a.IsNil() {
			return sharedRef(a.Elem(), b.Elem(), path)
		}
	}
	return ""
}
