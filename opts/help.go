package opts

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"text/tabwriter"
)

var positionalRegex = regexp.MustCompile(`\[([0-9]+)\]`)

func Help[T any](usage []string, err ...error) string {
	var t T
	positionals := []string{}
	fields := reflect.VisibleFields(reflect.TypeOf(t))
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, 1, ' ', 0)
	for _, field := range fields {
		tag := field.Tag.Get("opts")
		if tag == "" {
			continue
		}
		description := field.Tag.Get("desc")
		vals := strings.Split(tag, ",")
		if positionalRegex.MatchString(vals[0]) {
			positionals = append(positionals, strings.ToUpper(field.Name))
			continue
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", field.Name, strings.Join(vals, ", "), description)
	}
	w.Flush()
	output := ""
	if len(err) > 0 {
		output += "Error: " + err[0].Error() + "\n\n"
	}
	output += "Usage: " + strings.Join(usage, " ") + " [Options] " + strings.Join(positionals, " ") + "\n"
	output += "\n"
	output += "Options:\n"
	output += buffer.String()
	return output
}
