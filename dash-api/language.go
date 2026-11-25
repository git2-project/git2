package dashapi

import (
	"path"
	"strings"
)

// Blob.language: a Shiki language ID guessed from the file name, for Dash's
// highlighter. Unknown names give nil and Dash shows plain text. Names
// Shiki cannot guess from an extension use the same IDs as Dash's own
// fallback (web/apps/dash/src/lib/highlight.ts).

var languageByName = map[string]string{
	"dockerfile":     "docker",
	"containerfile":  "docker",
	"makefile":       "make",
	"gnumakefile":    "make",
	"cmakelists.txt": "cmake",
	"jenkinsfile":    "groovy",
	"go.mod":         "go",
	".gitignore":     "ini",
	".gitattributes": "ini",
	".editorconfig":  "ini",
	".env":           "dotenv",
	".bashrc":        "shellscript",
	".zshrc":         "shellscript",
	".profile":       "shellscript",
	"gemfile":        "ruby",
	"rakefile":       "ruby",
	"vagrantfile":    "ruby",
}

var languageByExt = map[string]string{
	"go":         "go",
	"ts":         "typescript",
	"mts":        "typescript",
	"cts":        "typescript",
	"tsx":        "tsx",
	"js":         "javascript",
	"mjs":        "javascript",
	"cjs":        "javascript",
	"jsx":        "jsx",
	"json":       "json",
	"jsonc":      "jsonc",
	"yaml":       "yaml",
	"yml":        "yaml",
	"toml":       "toml",
	"md":         "markdown",
	"markdown":   "markdown",
	"mdx":        "mdx",
	"py":         "python",
	"rb":         "ruby",
	"rs":         "rust",
	"java":       "java",
	"kt":         "kotlin",
	"kts":        "kotlin",
	"scala":      "scala",
	"groovy":     "groovy",
	"gradle":     "groovy",
	"c":          "c",
	"h":          "c",
	"cc":         "cpp",
	"cpp":        "cpp",
	"cxx":        "cpp",
	"hh":         "cpp",
	"hpp":        "cpp",
	"cs":         "csharp",
	"fs":         "fsharp",
	"swift":      "swift",
	"m":          "objective-c",
	"php":        "php",
	"pl":         "perl",
	"lua":        "lua",
	"r":          "r",
	"dart":       "dart",
	"ex":         "elixir",
	"exs":        "elixir",
	"erl":        "erlang",
	"hs":         "haskell",
	"clj":        "clojure",
	"zig":        "zig",
	"sh":         "shellscript",
	"bash":       "shellscript",
	"zsh":        "shellscript",
	"ps1":        "powershell",
	"bat":        "bat",
	"sql":        "sql",
	"html":       "html",
	"htm":        "html",
	"css":        "css",
	"scss":       "scss",
	"sass":       "sass",
	"less":       "less",
	"vue":        "vue",
	"svelte":     "svelte",
	"xml":        "xml",
	"svg":        "xml",
	"proto":      "proto",
	"graphql":    "graphql",
	"gql":        "graphql",
	"tf":         "terraform",
	"hcl":        "hcl",
	"ini":        "ini",
	"cfg":        "ini",
	"conf":       "ini",
	"properties": "properties",
	"diff":       "diff",
	"patch":      "diff",
	"dockerfile": "docker",
	"mk":         "make",
	"cmake":      "cmake",
	"nix":        "nix",
	"tex":        "latex",
	"csv":        "csv",
}

// languageFor returns the language of a file name (not a path), or nil.
func languageFor(name string) *string {
	lower := strings.ToLower(path.Base(name))
	if lang, ok := languageByName[lower]; ok {
		return &lang
	}
	// Dockerfile.dev, Dockerfile.prod
	if strings.HasPrefix(lower, "dockerfile.") || strings.HasPrefix(lower, "containerfile.") {
		lang := "docker"
		return &lang
	}
	if lang, ok := languageByExt[strings.TrimPrefix(path.Ext(lower), ".")]; ok {
		return &lang
	}
	return nil
}
