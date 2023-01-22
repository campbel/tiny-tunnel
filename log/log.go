package log

import (
	"fmt"
	"time"
)

type Pair [2]string

func Info(message string, args ...Pair) {
	m := [][2]string{}
	m = append(m, [2]string{"level", `"info"`})
	m = append(m, [2]string{"time", fmt.Sprintf(`"%s"`, time.Now().Format(time.RFC3339))})
	m = append(m, [2]string{"message", fmt.Sprintf(`"%s"`, message)})
	for _, v := range args {
		m = append(m, [2]string{v[0], v[1]})
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
