package opts

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

func ParseArgs[T any]() (T, error) {
	return Parse[T](os.Args[1:])
}

func MustParse[T any](usage []string, args []string) T {
	t, err := Parse[T](args)
	if err != nil {
		fmt.Println(Help[T](usage, err))
		os.Exit(1)
	}
	return t
}

func Parse[T any](args []string) (T, error) {
	var t T
	flags := getFlag[T]()
	posIndex := 0
	for i := 0; i < len(args); {
		var (
			fieldName    string
			fieldValue   string
			isPositional bool
			isSwitch     bool
		)
		fieldName, ok := flags[args[i]]
		if !ok {
			if args[i][0] == '-' {
				return t, errors.New("invalid flag " + args[i])
			}
			fieldName, ok = flags[fmt.Sprintf("[%d]", posIndex)]
			if !ok {
				return t, errors.New("missing positional argument " + fmt.Sprintf("%d", posIndex))
			}
			fieldValue = args[i]
			isPositional = true
		} else {
			if len(args) > i+1 {
				fieldValue = args[i+1]
			}
		}
		val := reflect.ValueOf(&t)
		field := val.Elem().FieldByName(fieldName)
		switch field.Kind() {
		case reflect.Int:
			if fieldValue == "" {
				return t, errors.New("missing value for int field " + fieldName)
			}
			val, err := strconv.ParseInt(fieldValue, 10, 64)
			if err != nil {
				return t, err
			}
			field.SetInt(val)
		case reflect.Bool:
			field.SetBool(true)
			isSwitch = true
		case reflect.String:
			if fieldValue == "" {
				return t, errors.New("missing value for string field " + fieldName)
			}
			field.SetString(fieldValue)
		case reflect.Array, reflect.Slice:
			if fieldValue == "" {
				return t, errors.New("missing value for array field " + fieldName)
			}
			if field.Type().Elem().Kind() != reflect.String {
				return t, errors.New("array value must be string")
			}
			if field.IsNil() {
				field.Set(reflect.MakeSlice(field.Type(), 0, 0))
			}
			field.Set(reflect.Append(field, reflect.ValueOf(fieldValue)))
		case reflect.Map:
			if fieldValue == "" {
				return t, errors.New("missing value for map field " + fieldName)
			}
			if field.Type().Key().Kind() != reflect.String {
				return t, errors.New("map key must be string")
			}
			if field.Type().Elem().Kind() != reflect.String {
				return t, errors.New("map value must be string")
			}
			if field.IsNil() {
				field.Set(reflect.MakeMap(field.Type()))
			}
			parts := strings.Split(fieldValue, "=")
			if len(parts) != 2 {
				return t, errors.New("invalid map value")
			}
			field.SetMapIndex(reflect.ValueOf(parts[0]), reflect.ValueOf(parts[1]))
		}
		if isPositional {
			posIndex += 1
		}
		if isPositional || isSwitch {
			i += 1
		} else {
			i += 2
		}
	}
	return t, nil
}

func getFlag[T any]() map[string]string {
	var t T
	flags := make(map[string]string)
	fields := reflect.VisibleFields(reflect.TypeOf(t))
	for _, field := range fields {
		tag := field.Tag.Get("opts")
		if tag == "" {
			continue
		}
		vals := strings.Split(tag, ",")
		for _, val := range vals {
			flags[val] = field.Name
		}
	}
	return flags
}
