package eco

import (
	"reflect"
	"testing"
)

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"package-lock.json": "{}",
		"Cargo.lock":        "",
		"uv.lock":           "",
		"poetry.lock":       "",
		"go.mod":            "module m\n",
		"yarn.lock":         "",
		"pnpm-lock.yaml":    "",
	})
	found, other := Detect(dir)
	want := []Source{{"npm", "package-lock.json"}, {"cargo", "Cargo.lock"}, {"python", "uv.lock"}, {"python", "poetry.lock"}, {"go", "go.mod"}}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("found = %v", found)
	}
	if !reflect.DeepEqual(other, []string{"yarn.lock", "pnpm-lock.yaml"}) {
		t.Errorf("other = %v", other)
	}

	shrink := t.TempDir()
	writeFiles(t, shrink, map[string]string{"package-lock.json": "{}", "npm-shrinkwrap.json": "{}"})
	if found, _ := Detect(shrink); !reflect.DeepEqual(found, []Source{{"npm", "npm-shrinkwrap.json"}}) {
		t.Errorf("shrinkwrap: %v", found)
	}

	if found, other := Detect(t.TempDir()); len(found) != 0 || len(other) != 0 {
		t.Errorf("empty dir: %v %v", found, other)
	}
}

func TestEcosystemSelectors(t *testing.T) {
	uv := Source{"python", "uv.lock"}
	poetry := Source{"python", "poetry.lock"}
	if !Ecosystems["python"](uv) || !Ecosystems["python"](poetry) {
		t.Error("python should select both")
	}
	if !Ecosystems["uv"](uv) || Ecosystems["uv"](poetry) {
		t.Error("uv selects only uv.lock")
	}
	if !Ecosystems["rust"](Source{"cargo", "Cargo.lock"}) || Ecosystems["npm"](uv) {
		t.Error("selector mismatch")
	}
}
