package log

import (
	"fmt"
	"strconv"
	"time"

	"github.com/campbel/tiny-tunnel/util"
)

// O represents a generic object
type Map map[string]any

// C represents a key/value pair
type Pair struct {
	Key   string
	Value any
}

func P(k string, v any) Pair {
	return Pair{Key: k, Value: v}
}

func Info(message string, args ...Pair) {
	m := [][2]string{}
	m = append(m, [2]string{"level", `"info"`})
	m = append(m, [2]string{"time", fmt.Sprintf(`"%s"`, time.Now().Format(time.RFC3339))})
	m = append(m, [2]string{"message", fmt.Sprintf(`"%s"`, message)})
	for _, v := range args {
		switch vv := v.Value.(type) {
		case string:
			m = append(m, [2]string{v.Key, fmt.Sprintf(`"%s"`, vv)})
		case bool:
			m = append(m, [2]string{v.Key, strconv.FormatBool(vv)})
		case int:
			m = append(m, [2]string{v.Key, strconv.Itoa(vv)})
		default:
			m = append(m, [2]string{v.Key, util.JSS(vv)})
		}
	}
	log(m)
}

func log(m [][2]string) {
	output := `{`
	for i, v := range m {
		if i > 0 {
			output += ","
		}
		output += fmt.Sprintf(`"%s":%s`, v[0], v[1])
	}
	output += `}`
	fmt.Println(output)
}
