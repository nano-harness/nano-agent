package skill

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

// CommandDef holds the definition of a slash command loaded from a .md file.
type CommandDef struct {
	Name                  string
	Namespace             string
	Description           string
	AllowedTools          []string
	PermissionProfile     string
	Source                string
	Body                  string
	PreludeTimeoutSeconds int
	PreludeOnError        string
	PreludeOutput         string
}

// CommandManager discovers and manages slash commands from .nano/commands and
// .claude/commands directories.
type CommandManager struct {
	cwd      string
	home     string
	commands map[string]*CommandDef
}

// commandFrontmatter is the YAML frontmatter format for command files.
type commandFrontmatter struct {
	Description           string   `yaml:"description"`
	AllowedTools          []string `yaml:"allowed-tools"`
	PermissionProfile     string   `yaml:"permission-profile"`
	PreludeTimeoutSeconds int      `yaml:"prelude_timeout"`
	PreludeOnError        string   `yaml:"prelude_on_error"`
	PreludeOutput         string   `yaml:"prelude_output"`
}

// NewCommandManager creates a new CommandManager rooted at cwd.
func NewCommandManager(cwd string) *CommandManager {
	home, _ := os.UserHomeDir()
	m := &CommandManager{cwd: cwd, home: home, commands: map[string]*CommandDef{}}
	m.loadAll()
	return m
}

func (m *CommandManager) dirs() []string {
	var ds []string
	if m.cwd != "" {
		ds = append(ds, filepath.Join(m.cwd, ".nano", "commands"))
		ds = append(ds, filepath.Join(m.cwd, ".claude", "commands"))
	}
	if m.home != "" {
		ds = append(ds, filepath.Join(m.home, ".nano", "commands"))
		ds = append(ds, filepath.Join(m.home, ".claude", "commands"))
	}
	return ds
}

func (m *CommandManager) loadAll() {
	for _, dir := range m.dirs() {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".md" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			body := string(b)
			var fm commandFrontmatter
			if strings.HasPrefix(body, "---\n") {
				idx := strings.Index(body[4:], "\n---")
				if idx >= 0 {
					_ = yaml.Unmarshal([]byte(body[4:4+idx]), &fm)
					body = body[4+idx+5:]
				}
			}
			name := commandNameFromPath(path)
			if _, exists := m.commands[name]; exists {
				return nil
			}
			ns := commandNamespaceOf(dir, path)
			src := commandSourceOf(dir, m.cwd, m.home)
			m.commands[name] = &CommandDef{
				Name:              name,
				Namespace:         ns,
				Description:       fm.Description,
				AllowedTools:      fm.AllowedTools,
				PermissionProfile: strings.TrimSpace(fm.PermissionProfile),
				Source:            src,
				Body:              trimCommandLeadingNewline(body),
				PreludeTimeoutSeconds: func() int {
					if fm.PreludeTimeoutSeconds > 0 {
						return fm.PreludeTimeoutSeconds
					}
					return 30
				}(),
				PreludeOnError: func() string {
					v := strings.ToLower(strings.TrimSpace(fm.PreludeOnError))
					if v == "abort" {
						return "abort"
					}
					return "continue"
				}(),
				PreludeOutput: func() string {
					switch strings.ToLower(strings.TrimSpace(fm.PreludeOutput)) {
					case "none", "summary", "full":
						return strings.ToLower(strings.TrimSpace(fm.PreludeOutput))
					}
					return "summary"
				}(),
			}
			return nil
		})
	}
}

// List returns all loaded command definitions.
func (m *CommandManager) List() []*CommandDef {
	out := make([]*CommandDef, 0, len(m.commands))
	for _, v := range m.commands {
		out = append(out, v)
	}
	return out
}

// Find returns a command definition by name.
func (m *CommandManager) Find(name string) (*CommandDef, bool) {
	v, ok := m.commands[name]
	return v, ok
}

// ─── rendering helpers ───────────────────────────────────────────────────────

// RenderCommand renders a command body with the provided arguments.
func RenderCommand(def *CommandDef, args []string) string {
	return RenderCommandBody(def.Body, args)
}

// RenderCommandBody substitutes $ARGUMENTS and $1..$N in body.
func RenderCommandBody(body string, args []string) string {
	all := strings.Join(args, " ")
	s := strings.ReplaceAll(body, "$ARGUMENTS", all)
	re := regexp.MustCompile(`\$(\d+)`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		idx, _ := strconv.Atoi(strings.TrimPrefix(m, "$"))
		if idx <= 0 || idx-1 >= len(args) {
			return ""
		}
		return args[idx-1]
	})
}

// ExtractCommandPreludes separates leading !<shell> lines from body.
func ExtractCommandPreludes(body string) ([]string, string) {
	lines := strings.Split(body, "\n")
	var preludes []string
	i := 0
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "!") {
			cmd := strings.TrimSpace(strings.TrimPrefix(l, "!"))
			if cmd != "" {
				preludes = append(preludes, cmd)
			}
			continue
		}
		break
	}
	return preludes, strings.Join(lines[i:], "\n")
}

// ParseSlashCommand parses a /command input string and returns the matching CommandDef.
func ParseSlashCommand(cwd string, input string) (*CommandDef, string, []string, bool) {
	in := strings.TrimSpace(input)
	if !strings.HasPrefix(in, "/") {
		return nil, "", nil, false
	}
	parts := splitCommandArgs(strings.TrimPrefix(in, "/"))
	if len(parts) == 0 {
		return nil, "", nil, false
	}
	m := NewCommandManager(cwd)
	def, ok := m.Find(parts[0])
	if !ok {
		return nil, "", nil, false
	}
	args := parts[1:]
	return def, RenderCommand(def, args), args, true
}

// ─── internal helpers ────────────────────────────────────────────────────────

func commandNameFromPath(p string) string {
	base := filepath.Base(p)
	if ext := filepath.Ext(base); ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

func commandNamespaceOf(root, path string) string {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "." || parts[0] == "" {
		return ""
	}
	return parts[0]
}

func commandSourceOf(dir, cwd, home string) string {
	switch {
	case strings.HasPrefix(dir, filepath.Join(cwd, ".nano")):
		return "project:nano"
	case strings.HasPrefix(dir, filepath.Join(cwd, ".claude")):
		return "project:claude"
	case strings.HasPrefix(dir, filepath.Join(home, ".nano")):
		return "user:nano"
	case strings.HasPrefix(dir, filepath.Join(home, ".claude")):
		return "user:claude"
	}
	return ""
}

func trimCommandLeadingNewline(s string) string {
	if len(s) > 0 && s[0] == '\n' {
		return s[1:]
	}
	return s
}

func splitCommandArgs(s string) []string {
	var res []string
	var cur strings.Builder
	inQuote := false
	quoteChar := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = true
			quoteChar = c
			continue
		}
		if c == ' ' || c == '\t' {
			if cur.Len() > 0 {
				res = append(res, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		res = append(res, cur.String())
	}
	return res
}
