// Package slash provides a unified registry for all slash commands supported
// by nano-agent. It aggregates built-in commands (permission, skill, schedule,
// openspec) with dynamically loaded custom commands so that every client
// (BubbleTea TUI, TView TUI, Daemon API) can obtain a single consistent list.
package slash

import (
	"sort"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/skill"
)

// Category classifies a slash command by its functional area.
type Category string

// Known categories.
const (
	CategoryPermission Category = "permission"
	CategorySkill      Category = "skill"
	CategoryAgent      Category = "agent"
	CategoryRoutines   Category = "routines"
	CategoryOpenSpec   Category = "openspec"
	CategoryCustom     Category = "custom"
)

// categoryOrder controls the display order of categories.
var categoryOrder = []Category{
	CategoryPermission,
	CategorySkill,
	CategoryAgent,
	CategoryRoutines,
	CategoryOpenSpec,
	CategoryCustom,
}

// Command describes a single slash command.
type Command struct {
	// Name is the command identifier without the leading slash,
	// e.g. "yolo", "skill:list", "loop".
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Usage       string   `json:"usage"`
	Category    Category `json:"category"`
	// Source is "built-in", "project:nano", "project:claude",
	// "user:nano", "user:claude", etc.
	Source string `json:"source"`
	// Namespace is the sub-directory namespace for custom commands.
	Namespace string `json:"namespace,omitempty"`
	// AllowedTools lists tool restrictions for custom commands.
	AllowedTools []string `json:"allowedTools,omitempty"`
}

// builtinCommands lists every hard-coded slash command.
var builtinCommands = []Command{
	// ── permission ───────────────────────────────────────────────────────────
	{
		Name:        "yolo",
		Description: "切换到 YOLO 模式，所有工具自动执行无需确认",
		Usage:       "/yolo",
		Category:    CategoryPermission,
		Source:      "built-in",
	},
	{
		Name:        "permission",
		Description: "设置权限模式",
		Usage:       "/permission <default|acceptEdits|yolo>",
		Category:    CategoryPermission,
		Source:      "built-in",
	},
	{
		Name:        "permissions",
		Description: "查看当前权限模式和 Session 白名单",
		Usage:       "/permissions",
		Category:    CategoryPermission,
		Source:      "built-in",
	},
	{
		Name:        "allow",
		Description: "添加 Session 白名单规则",
		Usage:       "/allow <rule>  例: /allow Bash(git *)",
		Category:    CategoryPermission,
		Source:      "built-in",
	},
	{
		Name:        "disallow",
		Description: "移除 Session 白名单规则",
		Usage:       "/disallow <rule>",
		Category:    CategoryPermission,
		Source:      "built-in",
	},
	{
		Name:        "think",
		Description: "切换思考模式（开启/关闭/查看状态）",
		Usage:       "/think [on|off|status]",
		Category:    CategoryPermission,
		Source:      "built-in",
	},
	{
		Name:        "clear",
		Description: "清空当前对话上下文，开启全新会话（等价于 Ctrl+R）",
		Usage:       "/clear",
		Category:    CategoryPermission,
		Source:      "built-in",
	},
	{
		Name:        "new",
		Description: "开启全新会话（/clear 的别名，等价于 Ctrl+R）",
		Usage:       "/new",
		Category:    CategoryPermission,
		Source:      "built-in",
	},

	// ── skill ────────────────────────────────────────────────────────────────
	{
		Name:        "skill:list",
		Description: "列出所有可用 Skill",
		Usage:       "/skill:list",
		Category:    CategorySkill,
		Source:      "built-in",
	},
	{
		Name:        "skill:use",
		Description: "启用指定 Skill",
		Usage:       "/skill:use <name>",
		Category:    CategorySkill,
		Source:      "built-in",
	},
	{
		Name:        "skill:off",
		Description: "禁用指定 Skill",
		Usage:       "/skill:off <name>",
		Category:    CategorySkill,
		Source:      "built-in",
	},
	{
		Name:        "skill:info",
		Description: "查看 Skill 详情",
		Usage:       "/skill:info <name>",
		Category:    CategorySkill,
		Source:      "built-in",
	},
	{
		Name:        "skill:install",
		Description: "从 URL 安装 Skill",
		Usage:       "/skill:install <url>",
		Category:    CategorySkill,
		Source:      "built-in",
	},

	// ── agent ────────────────────────────────────────────────────────────────
	{
		Name:        "teammates",
		Description: "列出当前团队的 teammate",
		Usage:       "/teammates",
		Category:    CategoryAgent,
		Source:      "built-in",
	},
	{
		Name:        "teammates:list",
		Description: "列出当前团队的 teammate（别名）",
		Usage:       "/teammates:list",
		Category:    CategoryAgent,
		Source:      "built-in",
	},
	{
		Name:        "teammates:show",
		Description: "显示特定 teammate 的详细信息",
		Usage:       "/teammates:show <name>",
		Category:    CategoryAgent,
		Source:      "built-in",
	},
	{
		Name:        "agents",
		Description: "列出当前团队的 teammate（兼容旧别名）",
		Usage:       "/agents",
		Category:    CategoryAgent,
		Source:      "built-in",
	},
	{
		Name:        "agents:list",
		Description: "列出当前团队的 teammate（兼容旧别名）",
		Usage:       "/agents:list",
		Category:    CategoryAgent,
		Source:      "built-in",
	},
	{
		Name:        "agents:show",
		Description: "显示特定 teammate 的详细信息（兼容旧别名）",
		Usage:       "/agents:show <name>",
		Category:    CategoryAgent,
		Source:      "built-in",
	},

	// ── routines ─────────────────────────────────────────────────────────────
	{
		Name:        "routines list",
		Description: "列出所有定时任务",
		Usage:       "/routines list",
		Category:    CategoryRoutines,
		Source:      "built-in",
	},
	{
		Name:        "routines add",
		Description: "添加定时任务（自然语言或 cron 表达式）",
		Usage:       "/routines add <description>  例: /routines add 每5分钟运行 go test",
		Category:    CategoryRoutines,
		Source:      "built-in",
	},
	{
		Name:        "routines remove",
		Description: "删除指定定时任务",
		Usage:       "/routines remove <id>",
		Category:    CategoryRoutines,
		Source:      "built-in",
	},
	{
		Name:        "routines status",
		Description: "查看定时任务运行状态",
		Usage:       "/routines status [id]",
		Category:    CategoryRoutines,
		Source:      "built-in",
	},
	{
		Name:        "routines pause",
		Description: "暂停定时任务",
		Usage:       "/routines pause <id>",
		Category:    CategoryRoutines,
		Source:      "built-in",
	},
	{
		Name:        "routines resume",
		Description: "恢复定时任务",
		Usage:       "/routines resume <id>",
		Category:    CategoryRoutines,
		Source:      "built-in",
	},

	// ── openspec ─────────────────────────────────────────────────────────────
	{
		Name:        "opsx:propose",
		Description: "创建新的 OpenSpec 变更提案",
		Usage:       "/opsx:propose <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:explore",
		Description: "探索并分析现有代码，生成变更提案",
		Usage:       "/opsx:explore <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:new",
		Description: "在现有提案基础上创建新任务",
		Usage:       "/opsx:new <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:continue",
		Description: "继续执行未完成的 OpenSpec 变更",
		Usage:       "/opsx:continue <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:ff",
		Description: "快进（Fast-Forward）执行 OpenSpec 变更",
		Usage:       "/opsx:ff <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:apply",
		Description: "应用 OpenSpec 变更",
		Usage:       "/opsx:apply <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:verify",
		Description: "验证 OpenSpec 变更",
		Usage:       "/opsx:verify <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:sync",
		Description: "同步 OpenSpec 变更状态",
		Usage:       "/opsx:sync <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:archive",
		Description: "归档 OpenSpec 变更",
		Usage:       "/opsx:archive <change-name>",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:status",
		Description: "查看 OpenSpec 变更状态",
		Usage:       "/opsx:status [change-name]",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
	{
		Name:        "opsx:bulk-archive",
		Description: "批量归档已完成的 OpenSpec 变更",
		Usage:       "/opsx:bulk-archive",
		Category:    CategoryOpenSpec,
		Source:      "built-in",
	},
}

// Registry aggregates built-in and custom slash commands.
type Registry struct {
	commands []Command
}

// NewRegistry constructs a Registry for the given working directory.
// Custom commands from .nano/commands and .claude/commands are loaded
// dynamically; built-in commands are always included.
func NewRegistry(cwd string) *Registry {
	r := newBuiltinRegistry()

	// Load custom commands via the existing CommandManager.
	mgr := skill.NewCommandManager(cwd)
	for _, d := range mgr.List() {
		r.commands = append(r.commands, Command{
			Name:         d.Name,
			Description:  d.Description,
			Usage:        "/" + d.Name,
			Category:     CategoryCustom,
			Source:       d.Source,
			Namespace:    d.Namespace,
			AllowedTools: d.AllowedTools,
		})
	}

	// Re-sort to place any added custom commands correctly.
	sort.Slice(r.commands, func(i, j int) bool {
		ci, cj := r.commands[i], r.commands[j]
		oi := categoryIndex(ci.Category)
		oj := categoryIndex(cj.Category)
		if oi != oj {
			return oi < oj
		}
		return ci.Name < cj.Name
	})

	return r
}

// NewBuiltinRegistry returns a Registry containing only hard-coded built-in
// commands, sorted in canonical order. Use this when custom command discovery
// (filesystem walk) is not appropriate (e.g. no working directory is known).
func NewBuiltinRegistry() *Registry {
	return newBuiltinRegistry()
}

// newBuiltinRegistry is the unexported implementation.
func newBuiltinRegistry() *Registry {
	r := &Registry{}
	r.commands = make([]Command, len(builtinCommands))
	copy(r.commands, builtinCommands)
	sort.Slice(r.commands, func(i, j int) bool {
		ci, cj := r.commands[i], r.commands[j]
		oi := categoryIndex(ci.Category)
		oj := categoryIndex(cj.Category)
		if oi != oj {
			return oi < oj
		}
		return ci.Name < cj.Name
	})
	return r
}

// All returns every registered command in canonical (category, name) order.
func (r *Registry) All() []Command {
	out := make([]Command, len(r.commands))
	copy(out, r.commands)
	return out
}

// ByCategory returns all commands belonging to the given category.
func (r *Registry) ByCategory(c Category) []Command {
	var out []Command
	for _, cmd := range r.commands {
		if cmd.Category == c {
			out = append(out, cmd)
		}
	}
	return out
}

// Search returns commands whose Name or Description contains query
// (case-insensitive substring match). An empty query returns all commands.
func (r *Registry) Search(query string) []Command {
	if query == "" {
		return r.All()
	}
	q := strings.ToLower(query)
	var out []Command
	for _, cmd := range r.commands {
		if strings.Contains(strings.ToLower(cmd.Name), q) ||
			strings.Contains(strings.ToLower(cmd.Description), q) {
			out = append(out, cmd)
		}
	}
	return out
}

// Names returns a slash-prefixed list of all command names suitable for
// Tab-completion, e.g. []string{"/yolo", "/skill:list", ...}.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.commands))
	for _, cmd := range r.commands {
		names = append(names, "/"+cmd.Name)
	}
	return names
}

// categoryIndex returns the display-order index for a category.
func categoryIndex(c Category) int {
	for i, cat := range categoryOrder {
		if cat == c {
			return i
		}
	}
	return len(categoryOrder)
}
