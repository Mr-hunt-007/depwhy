package eco

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const goModFile = `module example.com/app

go 1.22.0

toolchain go1.22.5

require (
	github.com/gin-gonic/gin v1.10.0
	"github.com/spf13/cobra" v1.8.1 // a comment
)

require (
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/text v0.15.0 // indirect
)

require golang.org/x/net v0.25.0 // indirect

replace golang.org/x/net => golang.org/x/net v0.24.0

exclude (
	golang.org/x/text v0.3.0
)
`

func TestParseGoMod(t *testing.T) {
	gm, err := ParseGoMod([]byte(goModFile))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]GoRequire{
		"github.com/gin-gonic/gin": {Version: "v1.10.0"},
		"github.com/spf13/cobra":   {Version: "v1.8.1"},
		"github.com/spf13/pflag":   {Version: "v1.0.5", Indirect: true},
		"golang.org/x/text":        {Version: "v0.15.0", Indirect: true},
		"golang.org/x/net":         {Version: "v0.25.0", Indirect: true},
	}
	if gm.Module != "example.com/app" || !reflect.DeepEqual(gm.Requires, want) {
		t.Errorf("got %+v", gm)
	}
	if _, err := ParseGoMod([]byte("go 1.22\n")); err == nil {
		t.Error("expected error without module directive")
	}
	if _, err := ParseGoMod([]byte("module x\nrequire (\n\tbroken\n)\n")); err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Errorf("err = %v", err)
	}
}

// Excerpt of real `go mod graph` output for a module using gin and cobra.
const goGraph = `example.com/app github.com/gin-gonic/gin@v1.10.0
example.com/app github.com/spf13/cobra@v1.8.1
example.com/app github.com/spf13/pflag@v1.0.5
example.com/app golang.org/x/text@v0.15.0
example.com/app go@1.22.0
example.com/app toolchain@go1.22.5
github.com/gin-gonic/gin@v1.10.0 golang.org/x/text@v0.15.0
github.com/gin-gonic/gin@v1.10.0 github.com/go-playground/locales@v0.14.1
github.com/gin-gonic/gin@v1.10.0 go@1.20
github.com/go-playground/locales@v0.14.1 golang.org/x/text@v0.3.8
github.com/spf13/cobra@v1.8.1 github.com/spf13/pflag@v1.0.5
github.com/spf13/cobra@v1.8.1 gopkg.in/yaml.v3@v3.0.1
golang.org/x/text@v0.3.8 golang.org/x/tools@v0.1.12
golang.org/x/text@v0.15.0 golang.org/x/tools@v0.0.0-20180917221912-90fa682c2a6e
`

func TestBuildGo(t *testing.T) {
	gm, err := ParseGoMod([]byte("module example.com/app\nrequire (\n\tgithub.com/gin-gonic/gin v1.10.0\n\tgithub.com/spf13/cobra v1.8.1\n\tgithub.com/spf13/pflag v1.0.5 // indirect\n\tgolang.org/x/text v0.15.0 // indirect\n\tgopkg.in/yaml.v3 v3.0.1 // indirect\n)\n"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGo("go.mod", []byte(goGraph), gm)
	if err != nil {
		t.Fatal(err)
	}
	if v := g.Nodes["golang.org/x/text"].Version; v != "v0.15.0" {
		t.Errorf("selected x/text = %s", v)
	}
	if v := g.Nodes["golang.org/x/tools"].Version; v != "v0.1.12" {
		t.Errorf("selected x/tools = %s", v)
	}
	if _, ok := g.Nodes["go"]; ok {
		t.Error("go@ pseudo-module should be skipped")
	}
	tests := []struct {
		name string
		want []string
	}{
		// The indirect requirement edge from the main module is only used
		// when nothing else explains the module.
		{"golang.org/x/text", []string{
			"example.com/app > github.com/gin-gonic/gin@v1.10.0 > golang.org/x/text@v0.15.0",
			"example.com/app > github.com/gin-gonic/gin@v1.10.0 > github.com/go-playground/locales@v0.14.1 > golang.org/x/text@v0.15.0",
		}},
		{"github.com/spf13/pflag", []string{"example.com/app > github.com/spf13/cobra@v1.8.1 > github.com/spf13/pflag@v1.0.5"}},
		// x/tools is only required by x/text v0.3.8, which was not selected,
		// and by the selected v0.15.0.
		{"golang.org/x/tools", []string{
			"example.com/app > github.com/gin-gonic/gin@v1.10.0 > golang.org/x/text@v0.15.0 > golang.org/x/tools@v0.1.12",
			"example.com/app > github.com/gin-gonic/gin@v1.10.0 > github.com/go-playground/locales@v0.14.1 > golang.org/x/text@v0.15.0 > golang.org/x/tools@v0.1.12",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathsOf(t, g, tt.name); !sameSet(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
	if got := strings.Join(g.Nodes["golang.org/x/text"].Flags, ","); got != "indirect" {
		t.Errorf("flags = %q", got)
	}
}

func TestBuildGoIndirectOnly(t *testing.T) {
	gm, _ := ParseGoMod([]byte("module m\nrequire example.com/orphan v1.0.0 // indirect\n"))
	g, err := BuildGo("go.mod", []byte("m example.com/orphan@v1.0.0\n"), gm)
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(t, g, "example.com/orphan"); !sameSet(got, []string{"m > example.com/orphan@v1.0.0[indirect]"}) {
		t.Errorf("got %q", got)
	}
}

func TestBuildGoErrors(t *testing.T) {
	if _, err := BuildGo("go.mod", []byte("a b c\n"), nil); err == nil {
		t.Error("expected error for three fields")
	}
	if _, err := BuildGo("go.mod", []byte("a b\n"), nil); err == nil {
		t.Error("expected error for requirement without version")
	}
}

// TestGoModGraphIntegration runs the real go command against local modules
// connected by replace directives, so no network is needed.
func TestGoModGraphIntegration(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not available")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"app/go.mod": `module example.com/app

go 1.21

require (
	example.com/direct v1.0.0
	example.com/leaf v1.2.0 // indirect
)

replace (
	example.com/direct => ../direct
	example.com/leaf => ../leaf
)
`,
		"direct/go.mod": "module example.com/direct\n\ngo 1.21\n\nrequire example.com/leaf v1.2.0\n",
		"leaf/go.mod":   "module example.com/leaf\n\ngo 1.21\n",
	})
	g, err := Load(dir+"/app", Source{Ecosystem: "go", File: "go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(t, g, "example.com/leaf"); !sameSet(got, []string{"example.com/app > example.com/direct@v1.0.0 > example.com/leaf@v1.2.0"}) {
		t.Errorf("got %q", got)
	}
}

func TestGoMissing(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"go.mod": "module m\n"})
	t.Setenv("PATH", t.TempDir())
	_, err := Load(dir, Source{Ecosystem: "go", File: "go.mod"})
	if err != ErrGoMissing {
		t.Errorf("err = %v, want ErrGoMissing", err)
	}
}
