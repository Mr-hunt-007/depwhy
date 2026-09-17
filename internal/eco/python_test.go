package eco

import (
	"strings"
	"testing"
)

func TestPEP503(t *testing.T) {
	tests := map[string]string{
		"requests":            "requests",
		"Flask":               "flask",
		"typing_extensions":   "typing-extensions",
		"zope.interface":      "zope-interface",
		"Foo__Bar.-baz":       "foo-bar-baz",
		" charset-normalizer": "charset-normalizer",
	}
	for in, want := range tests {
		if got := PEP503(in); got != want {
			t.Errorf("PEP503(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPEP508Name(t *testing.T) {
	tests := map[string]string{
		"requests>=2.31":                        "requests",
		"requests[socks] ; python_version<'3'":  "requests",
		"numpy<2; python_version<'3.12'":        "numpy",
		"zope.interface (>=5)":                  "zope.interface",
		"my_pkg @ git+https://example.com/x":    "my_pkg",
		"  Flask==3.0.3":                        "Flask",
		"backports.zoneinfo~=0.2":               "backports.zoneinfo",
		"importlib-metadata!=4.7.0,>=4.4":       "importlib-metadata",
		"pytest":                                "pytest",
		"pywin32; sys_platform == 'win32'":      "pywin32",
		"typing_extensions[extra1,extra2]>=4.0": "typing_extensions",
	}
	for in, want := range tests {
		if got := pep508Name(in); got != want {
			t.Errorf("pep508Name(%q) = %q, want %q", in, got, want)
		}
	}
}

// Trimmed from a real uv.lock written by uv 0.12 (sdist and wheel entries
// shortened).
const uvLock = `version = 1
revision = 3
requires-python = ">=3.10"
resolution-markers = [
    "python_full_version >= '3.12'",
    "python_full_version < '3.12'",
]

[[package]]
name = "flask"
version = "3.0.3"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "markupsafe" },
    { name = "werkzeug" },
]
sdist = { url = "https://files.pythonhosted.org/packages/flask-3.0.3.tar.gz", hash = "sha256:ab", size = 676315, upload-time = "2024-04-07T19:26:11.035Z" }

[[package]]
name = "markupsafe"
version = "3.0.3"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "my-app"
version = "0.3.0"
source = { virtual = "." }
dependencies = [
    { name = "flask" },
    { name = "numpy", version = "1.26.4", source = { registry = "https://pypi.org/simple" }, marker = "python_full_version < '3.12'" },
    { name = "numpy", version = "2.5.3", source = { registry = "https://pypi.org/simple" }, marker = "python_full_version >= '3.12'" },
    { name = "requests" },
]

[package.optional-dependencies]
socks = [
    { name = "requests", extra = ["socks"] },
]

[package.dev-dependencies]
dev = [
    { name = "pytest" },
]
docs = [
    { name = "mkdocs" },
]

[package.metadata]
requires-dist = [
    { name = "flask", specifier = "==3.0.3" },
    { name = "requests", specifier = ">=2.31" },
]

[package.metadata.requires-dev]
dev = [{ name = "pytest", specifier = ">=8" }]

[[package]]
name = "mkdocs"
version = "1.6.1"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "markupsafe" },
    { name = "pyyaml" },
]

[[package]]
name = "numpy"
version = "1.26.4"
source = { registry = "https://pypi.org/simple" }
resolution-markers = [
    "python_full_version < '3.12'",
]

[[package]]
name = "numpy"
version = "2.5.3"
source = { registry = "https://pypi.org/simple" }
resolution-markers = [
    "python_full_version >= '3.12'",
]

[[package]]
name = "pysocks"
version = "1.7.1"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "pytest"
version = "9.1.1"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "colorama", marker = "sys_platform == 'win32'" },
]

[[package]]
name = "colorama"
version = "0.4.6"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "pyyaml"
version = "6.0.3"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "requests"
version = "2.34.2"
source = { registry = "https://pypi.org/simple" }

[package.optional-dependencies]
socks = [
    { name = "pysocks" },
]

[[package]]
name = "werkzeug"
version = "3.1.8"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "markupsafe" },
]
`

func TestUV(t *testing.T) {
	g, err := BuildUV("uv.lock", []byte(uvLock), nil, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Warnings) != 0 {
		t.Errorf("warnings: %v", g.Warnings)
	}
	tests := []struct {
		name string
		want []string
	}{
		{"PySocks", []string{"my-app@0.3.0 > requests@2.34.2 > pysocks@1.7.1[extra:socks]"}},
		{"numpy", []string{"my-app@0.3.0 > numpy@1.26.4", "my-app@0.3.0 > numpy@2.5.3"}},
		{"MarkupSafe", []string{
			"my-app@0.3.0 > flask@3.0.3 > markupsafe@3.0.3",
			"my-app@0.3.0 > mkdocs@1.6.1[group:docs] > markupsafe@3.0.3",
			"my-app@0.3.0 > flask@3.0.3 > werkzeug@3.1.8 > markupsafe@3.0.3",
		}},
		{"colorama", []string{"my-app@0.3.0 > pytest@9.1.1[dev] > colorama@0.4.6"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathsOf(t, g, tt.name); !sameSet(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestUVWorkspaceMembers(t *testing.T) {
	lock := `version = 1
revision = 3

[manifest]
members = ["app", "lib"]

[[package]]
name = "app"
version = "0.1.0"
source = { editable = "packages/app" }
dependencies = [{ name = "lib" }]

[[package]]
name = "lib"
version = "0.1.0"
source = { editable = "packages/lib" }
dependencies = [{ name = "idna" }]

[[package]]
name = "idna"
version = "3.10"
source = { registry = "https://pypi.org/simple" }
`
	g, err := BuildUV("uv.lock", []byte(lock), nil, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(t, g, "idna"); !sameSet(got, []string{"lib@0.1.0 > idna@3.10"}) {
		t.Errorf("got %q", got)
	}
}

func TestUVFallbackToPyProject(t *testing.T) {
	lock := `version = 1

[[package]]
name = "idna"
version = "3.10"
source = { registry = "https://pypi.org/simple" }
`
	pp, err := ParsePyProject([]byte("[project]\nname = \"svc\"\ndependencies = [\"IDNA>=3\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildUV("uv.lock", []byte(lock), pp, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(t, g, "idna"); !sameSet(got, []string{"svc > idna@3.10"}) {
		t.Errorf("got %q", got)
	}
}

func TestParsePyProject(t *testing.T) {
	data := `[project]
name = "My_App"
version = "1.0"
dependencies = ["requests>=2", "numpy<2; python_version<'3.12'"]

[project.optional-dependencies]
socks = ["PySocks"]

[dependency-groups]
dev = ["pytest", {include-group = "lint"}]
lint = ["ruff", {include-group = "dev"}]

[tool.poetry.dependencies]
python = "^3.10"
Flask = "3.0.3"
PyYAML = { version = "^6.0", optional = true }

[tool.poetry.dev-dependencies]
black = "*"

[tool.poetry.group.docs.dependencies]
mkdocs = "*"

[tool.uv]
dev-dependencies = ["coverage"]
`
	pp, err := ParsePyProject([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range pp.deps {
		got = append(got, d.name+"/"+d.kind)
	}
	want := []string{
		"requests/", "numpy/", "PySocks/extra:socks",
		"Flask/", "PyYAML/optional", "black/dev", "mkdocs/group:docs",
		"pytest/dev", "ruff/dev", "ruff/group:lint", "pytest/group:lint",
		"coverage/dev",
	}
	if !sameSet(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if pp.Name != "My_App" || pp.Version != "1.0" {
		t.Errorf("name/version = %q %q", pp.Name, pp.Version)
	}
	if _, err := ParsePyProject([]byte("[project]\nname = \n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("err = %v", err)
	}
}

// Trimmed from a real poetry.lock written by Poetry 2.4 (file hashes removed).
const poetryLock = `# This file is automatically @generated by Poetry 2.4.3 and should not be changed by hand.

[[package]]
name = "colorama"
version = "0.4.6"
description = "Cross-platform colored terminal text."
optional = false
python-versions = "!=3.0.*,!=3.1.*,!=3.2.*,!=3.3.*,!=3.4.*,!=3.5.*,!=3.6.*,>=2.7"
groups = ["dev", "docs"]
markers = "sys_platform == \"win32\" or platform_system == \"Windows\""
files = [
    {file = "colorama-0.4.6-py2.py3-none-any.whl", hash = "sha256:4f1d"},
]

[[package]]
name = "charset-normalizer"
version = "3.5.1"
description = "The Real First Universal Charset Detector."
optional = false
python-versions = ">=3.7"
groups = ["main"]
files = []

[[package]]
name = "mkdocs"
version = "1.6.1"
description = "Project documentation with Markdown."
optional = false
python-versions = ">=3.8"
groups = ["docs"]
files = []

[package.dependencies]
colorama = {version = ">=0.4", markers = "platform_system == \"Windows\""}
pyyaml = ">=5.1"

[package.extras]
i18n = ["babel (>=2.9.0)"]

[[package]]
name = "pytest"
version = "8.4.2"
description = "pytest: simple powerful testing with Python"
optional = false
python-versions = ">=3.9"
groups = ["dev"]
files = []

[package.dependencies]
colorama = {version = ">=0.4", markers = "sys_platform == \"win32\""}

[[package]]
name = "pyyaml"
version = "6.0.3"
description = "YAML parser and emitter for Python"
optional = false
python-versions = ">=3.8"
groups = ["main", "docs"]
files = []

[[package]]
name = "pysocks"
version = "1.7.1"
description = "A Python SOCKS client module."
optional = true
python-versions = "*"
groups = ["main"]
files = []

[[package]]
name = "requests"
version = "2.34.2"
description = "Python HTTP for Humans."
optional = false
python-versions = ">=3.10"
groups = ["main"]
files = []

[package.dependencies]
charset_normalizer = ">=2,<4"
PySocks = {version = ">=1.5.6,!=1.5.7", optional = true, markers = "extra == \"socks\""}

[package.extras]
socks = ["PySocks (>=1.5.6,!=1.5.7)"]

[extras]
yaml = ["PyYAML"]

[metadata]
lock-version = "2.1"
python-versions = "^3.10"
content-hash = "a5a969131954c2d3985c22e0104202266fda10aa96a04ddaff7397c7f360835b"
`

const poetryPyProject = `[tool.poetry]
name = "legacy-svc"
version = "1.2.0"
description = "x"
authors = ["a <a@example.com>"]
package-mode = false

[tool.poetry.dependencies]
python = "^3.10"
requests = { version = "^2.31", extras = ["socks"] }
PyYAML = { version = "^6.0", optional = true }

[tool.poetry.extras]
yaml = ["PyYAML"]

[tool.poetry.group.dev.dependencies]
pytest = "^8"

[tool.poetry.group.docs.dependencies]
mkdocs = "*"
`

func TestPoetry(t *testing.T) {
	pp, err := ParsePyProject([]byte(poetryPyProject))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildPoetry("poetry.lock", []byte(poetryLock), pp, "dir")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		want []string
	}{
		{"charset_normalizer", []string{"legacy-svc@1.2.0 > requests@2.34.2 > charset-normalizer@3.5.1"}},
		{"colorama", []string{"legacy-svc@1.2.0 > mkdocs@1.6.1[group:docs] > colorama@0.4.6", "legacy-svc@1.2.0 > pytest@8.4.2[dev] > colorama@0.4.6"}},
		{"pyyaml", []string{"legacy-svc@1.2.0 > pyyaml@6.0.3[optional]", "legacy-svc@1.2.0 > mkdocs@1.6.1[group:docs] > pyyaml@6.0.3"}},
		{"pysocks", []string{"legacy-svc@1.2.0 > requests@2.34.2 > pysocks@1.7.1[optional]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathsOf(t, g, tt.name); !sameSet(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
	flags := map[string]string{"colorama 0.4.6": "group:dev,docs", "pytest 8.4.2": "dev", "pysocks 1.7.1": "optional", "pyyaml 6.0.3": ""}
	for id, want := range flags {
		if got := strings.Join(g.Nodes[id].Flags, ","); got != want {
			t.Errorf("%s flags = %q, want %q", id, got, want)
		}
	}
}

func TestPoetryWithoutPyProject(t *testing.T) {
	g, err := BuildPoetry("poetry.lock", []byte(poetryLock), nil, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Warnings) != 1 || !strings.Contains(g.Warnings[0], "pyproject.toml not found") {
		t.Errorf("warnings = %v", g.Warnings)
	}
	if got := pathsOf(t, g, "charset-normalizer"); !sameSet(got, []string{"requests@2.34.2 > charset-normalizer@3.5.1"}) {
		t.Errorf("got %q", got)
	}
}

func TestPoetryLegacyCategory(t *testing.T) {
	lock := `[[package]]
name = "pytest"
version = "7.0.0"
description = ""
category = "dev"
optional = false
python-versions = ">=3.6"

[package.dependencies]
py = ">=1.8.2"
tomli = [
    {version = ">=1.0.0", markers = "python_version < \"3.11\""},
]

[[package]]
name = "py"
version = "1.11.0"
description = ""
category = "dev"
optional = false
python-versions = "*"

[[package]]
name = "tomli"
version = "2.0.1"
description = ""
category = "dev"
optional = false
python-versions = ">=3.7"

[metadata]
lock-version = "1.1"
python-versions = "^3.8"
content-hash = "x"

[metadata.files]
py = []
`
	pp, _ := ParsePyProject([]byte("[tool.poetry]\nname = \"old\"\n[tool.poetry.dev-dependencies]\npytest = \"^7\"\n"))
	g, err := BuildPoetry("poetry.lock", []byte(lock), pp, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if got := pathsOf(t, g, "tomli"); !sameSet(got, []string{"old > pytest@7.0.0[dev] > tomli@2.0.1"}) {
		t.Errorf("got %q", got)
	}
	if got := strings.Join(g.Nodes["tomli 2.0.1"].Flags, ","); got != "dev" {
		t.Errorf("flags = %q", got)
	}
}
