package opts

import (
	"reflect"
	"strconv"
	"strings"
)

func WithDefaults[T any]() T {
	var t T
	if reflect.ValueOf(t).Kind() != reflect.Struct {
		return t
	}
	fields := reflect.VisibleFields(reflect.TypeOf(t))
	for _, field := range fields {
		tag := field.Tag.Get("default")
		if tag == "" {
			continue
		}
		val := reflect.ValueOf(&t)
		field := val.Elem().FieldByName(field.Name)
		switch field.Kind() {
		case reflect.String:
			field.SetString(tag)
		case reflect.Int, reflect.Int64:
			v, _ := strconv.Atoi(tag)
			field.SetInt(int64(v))
		case reflect.Bool:
			v, _ := strconv.ParseBool(tag)
			field.SetBool(v)
		case reflect.Array, reflect.Slice:
			if field.IsNil() {
				field.Set(reflect.MakeSlice(field.Type(), 0, 0))
			}
			parts := strings.Split(tag, ",")
			for _, part := range parts {
				switch field.Type().Elem().Kind() {
				case reflect.String:
					field.Set(reflect.Append(field, reflect.ValueOf(part)))
				case reflect.Int, reflect.Int64:
					v, _ := strconv.Atoi(part)
					field.Set(reflect.Append(field, reflect.ValueOf(v)))
				case reflect.Bool:
					v, _ := strconv.ParseBool(part)
					field.Set(reflect.Append(field, reflect.ValueOf(v)))
				}
			}
		case reflect.Map:
			if field.Type().Key().Kind() != reflect.String {
				continue
			}
			if field.IsNil() {
				field.Set(reflect.MakeMap(field.Type()))
			}
			parts := strings.Split(tag, ",")
			for _, part := range parts {
				p := strings.Split(part, "=")
				if len(p) != 2 {
					continue
				}
				switch field.Type().Elem().Kind() {
				case reflect.String:
					field.SetMapIndex(reflect.ValueOf(p[0]), reflect.ValueOf(p[1]))
				case reflect.Int, reflect.Int64:
					v, _ := strconv.Atoi(p[1])
					field.SetMapIndex(reflect.ValueOf(p[0]), reflect.ValueOf(v))
				case reflect.Bool:
					v, _ := strconv.ParseBool(p[1])
					field.SetMapIndex(reflect.ValueOf(p[0]), reflect.ValueOf(v))
				}
			}

		}
	}
	return t
}
