package dashapi

import "testing"

func TestLanguageFor(t *testing.T) {
	cases := map[string]string{
		"main.go":        "go",
		"App.TSX":        "tsx",
		"README.md":      "markdown",
		"ci.yml":         "yaml",
		"Dockerfile":     "docker",
		"Dockerfile.dev": "docker",
		"Makefile":       "make",
		"go.mod":         "go",
		".gitignore":     "ini",
		".env":           "dotenv",
		"build.sh":       "shellscript",
		"CMakeLists.txt": "cmake",
		"logo.svg":       "xml",
	}
	for name, want := range cases {
		got := languageFor(name)
		if got == nil || *got != want {
			t.Errorf("languageFor(%q) = %v, want %q", name, got, want)
		}
	}
	for _, name := range []string{"LICENSE", "notes.txt", "data.bin", "archive.tar.gz", ""} {
		if got := languageFor(name); got != nil {
			t.Errorf("languageFor(%q) = %q, want nil", name, *got)
		}
	}
}
