package opts

import (
	"reflect"
	"testing"
)

type TestOpts struct {
	Pos1   string            `opts:"[0]"`
	Pos2   int               `opts:"[1]"`
	Name   string            `opts:"-n,--name"`
	Count  int               `opts:"-c,--count"`
	Switch bool              `opts:"-s,--switch"`
	Items  []string          `opts:"-i,--item"`
	Dict   map[string]string `opts:"-d,--dict"`
}

func TestParse(t *testing.T) {
	tt := []struct {
		name    string
		args    []string
		want    TestOpts
		wantErr bool
	}{
		{
			name: "positional args",
			args: []string{"foo", "42"},
			want: TestOpts{Pos1: "foo", Pos2: 42},
		},
		{
			name: "string flag",
			args: []string{"-n", "foo"},
			want: TestOpts{Name: "foo"},
		},
		{
			name: "int flag",
			args: []string{"-c", "42"},
			want: TestOpts{Count: 42},
		},
		{
			name: "bool flags",
			args: []string{"-s"},
			want: TestOpts{Switch: true},
		},
		{
			name: "map flags",
			args: []string{"-d", "foo=bar"},
			want: TestOpts{Dict: map[string]string{"foo": "bar"}},
		},
		{
			name: "slice flags",
			args: []string{"-i", "foo", "-i", "bar"},
			want: TestOpts{Items: []string{"foo", "bar"}},
		},
		{
			name: "all together",
			args: []string{"-n", "foo", "-c", "42", "-s", "-i", "foo", "-i", "bar", "-d", "foo=bar", "pos1", "42"},
			want: TestOpts{Name: "foo", Count: 42, Switch: true, Items: []string{"foo", "bar"}, Dict: map[string]string{"foo": "bar"}, Pos1: "pos1", Pos2: 42},
		},
		{
			name: "all together with long flags",
			args: []string{"--name", "foo", "--count", "42", "--switch", "--item", "foo", "--item", "bar", "--dict", "foo=bar", "pos1", "42"},
			want: TestOpts{Name: "foo", Count: 42, Switch: true, Items: []string{"foo", "bar"}, Dict: map[string]string{"foo": "bar"}, Pos1: "pos1", Pos2: 42},
		},
		{
			name:    "malformed",
			args:    []string{"foobar", "--buzz"},
			wantErr: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			testParse(t, tc.args, tc.want, tc.wantErr)
		})
	}
}

func testParse(t *testing.T, args []string, want TestOpts, wantErr bool) {
	t.Helper()
	got, err := Parse[TestOpts](args)
	if wantErr && err != nil {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != want.Count {
		t.Errorf("got %v, want %v", got.Count, want.Count)
	}
	if got.Name != want.Name {
		t.Errorf("got %v, want %v", got.Name, want.Name)
	}
	if got.Pos1 != want.Pos1 {
		t.Errorf("got %v, want %v", got.Pos1, want.Pos1)
	}
	if got.Pos2 != want.Pos2 {
		t.Errorf("got %v, want %v", got.Pos2, want.Pos2)
	}
	if got.Switch != want.Switch {
		t.Errorf("got %v, want %v", got.Switch, want.Switch)
	}
	if !reflect.DeepEqual(got.Dict, want.Dict) {
		t.Errorf("got %v, want %v", got.Dict, want.Dict)
	}
}
