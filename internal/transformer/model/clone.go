package model

import "reflect"

// Clone returns a type-preserving deep copy of the request. Runtime-only
// fields are copied as well; cloning does not use JSON serialization.
func (r *InternalLLMRequest) Clone() *InternalLLMRequest {
	if r == nil {
		return nil
	}
	return deepClone(r).(*InternalLLMRequest)
}

// Clone returns a type-preserving deep copy of the message.
func (m Message) Clone() Message {
	return deepClone(m).(Message)
}

// CloneMessages returns a deep copy of messages.
func CloneMessages(messages []Message) []Message {
	if messages == nil {
		return nil
	}
	return deepClone(messages).([]Message)
}

type cloneVisit struct {
	typ  reflect.Type
	kind reflect.Kind
	ptr  uintptr
	len  int
	cap  int
}

func deepClone(value any) any {
	return cloneValue(reflect.ValueOf(value), make(map[cloneVisit]reflect.Value)).Interface()
}

func cloneValue(src reflect.Value, visited map[cloneVisit]reflect.Value) reflect.Value {
	if !src.IsValid() {
		return src
	}

	switch src.Kind() {
	case reflect.Interface:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		cloned := cloneValue(src.Elem(), visited)
		dst := reflect.New(src.Type()).Elem()
		dst.Set(cloned)
		return dst

	case reflect.Pointer:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		visit := cloneVisit{typ: src.Type(), kind: src.Kind(), ptr: src.Pointer()}
		if cloned, ok := visited[visit]; ok {
			return cloned
		}
		dst := reflect.New(src.Type().Elem())
		visited[visit] = dst
		dst.Elem().Set(cloneValue(src.Elem(), visited))
		return dst

	case reflect.Slice:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		visit := cloneVisit{typ: src.Type(), kind: src.Kind(), ptr: src.Pointer(), len: src.Len(), cap: src.Cap()}
		if cloned, ok := visited[visit]; ok {
			return cloned
		}
		dst := reflect.MakeSlice(src.Type(), src.Len(), src.Cap())
		visited[visit] = dst
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(cloneValue(src.Index(i), visited))
		}
		return dst

	case reflect.Map:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		visit := cloneVisit{typ: src.Type(), kind: src.Kind(), ptr: src.Pointer()}
		if cloned, ok := visited[visit]; ok {
			return cloned
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		visited[visit] = dst
		iter := src.MapRange()
		for iter.Next() {
			dst.SetMapIndex(cloneValue(iter.Key(), visited), cloneValue(iter.Value(), visited))
		}
		return dst

	case reflect.Struct:
		// Start with a value copy so unexported implementation fields retain their
		// value. Exported fields are then replaced with independent deep copies.
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src)
		typ := src.Type()
		for i := 0; i < src.NumField(); i++ {
			if typ.Field(i).PkgPath != "" {
				continue
			}
			dst.Field(i).Set(cloneValue(src.Field(i), visited))
		}
		return dst

	case reflect.Array:
		dst := reflect.New(src.Type()).Elem()
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(cloneValue(src.Index(i), visited))
		}
		return dst

	default:
		return src
	}
}
