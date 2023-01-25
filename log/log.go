package log

import (
	"fmt"
	"time"

	"github.com/campbel/tiny-tunnel/util"
)

type Level string

var (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
)

type Map map[string]any

type Pair struct {
	Key   string
	Value any
}

func P(k string, v any) Pair {
	return Pair{Key: k, Value: v}
}

var level = LevelInfo

func SetLevel(l Level) {
	level = l
}

func SetDebug() {
	SetLevel(LevelDebug)
}

func Debug(message string, args ...Pair) {
	if level == LevelDebug {
		Msg(LevelDebug, message, args...)
	}
}

func Info(message string, args ...Pair) {
	Msg(LevelInfo, message, args...)
}

func Msg(level Level, message string, args ...Pair) {
	output := `{`
	args = append([]Pair{P("level", level), P("time", time.Now().Format(time.RFC3339)), P("message", message)}, args...)
	for i, v := range args {
		if i > 0 {
			output += ","
		}
		switch vv := v.Value.(type) {
		case string:
			output += fmt.Sprintf(`"%s":"%s"`, v.Key, vv)
		case bool:
			output += fmt.Sprintf(`"%s":%t`, v.Key, vv)
		case int:
			output += fmt.Sprintf(`"%s":%d`, v.Key, vv)
		default:
			output += fmt.Sprintf(`"%s":%s`, v.Key, util.JSS(vv))
		}
	}
	output += `}`
	fmt.Println(output)
}
