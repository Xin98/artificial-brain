package dto

import (
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Public is the representation boundary: currencies/prices/shares stay decimal strings and ownership stays internal.
func Public(value any) any { return publicValue(reflect.ValueOf(value)) }
func PublicName(name string) string {
	for _, a := range []string{"TTM", "SMA", "RSI", "NAV", "CIK", "SIC", "EPS", "ROE", "USD", "ID", "PE", "PB"} {
		name = strings.ReplaceAll(name, a, a[:1]+strings.ToLower(a[1:]))
	}
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}
func publicValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return publicValue(v.Elem())
	}
	if v.CanInterface() {
		switch x := v.Interface().(type) {
		case domain.Money:
			return x.String()
		case domain.Price:
			return x.String()
		case domain.Quantity:
			return x.String()
		case time.Time:
			return x.UTC().Format(time.RFC3339Nano)
		case json.RawMessage:
			var out any
			_ = json.Unmarshal(x, &out)
			return out
		case domain.Metric:
			var value any
			if x.Value != nil {
				value = strconv.FormatFloat(*x.Value, 'f', -1, 64)
			}
			refs := x.FactRefs
			if refs == nil {
				refs = []string{}
			}
			return map[string]any{"value": value, "reason": x.Reason, "factRefs": refs}
		}
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{}
		t := v.Type()
		for n := 0; n < v.NumField(); n++ {
			f := t.Field(n)
			if f.PkgPath != "" || f.Name == "Scope" {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			if len(tag) > 1 && tag[1] == "omitempty" && v.Field(n).IsZero() {
				continue
			}
			val := publicValue(v.Field(n))
			if f.Anonymous && tag[0] == "" {
				if m, ok := val.(map[string]any); ok {
					for k, x := range m {
						out[k] = x
					}
					continue
				}
			}
			name := tag[0]
			if name == "" {
				name = PublicName(f.Name)
			}
			out[name] = val
		}
		return out
	case reflect.Slice, reflect.Array:
		out := []any{}
		for n := 0; n < v.Len(); n++ {
			out = append(out, publicValue(v.Index(n)))
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			if key == "scope" || key == "Scope" {
				continue
			}
			out[key] = publicValue(iter.Value())
		}
		return out
	default:
		return v.Interface()
	}
}
