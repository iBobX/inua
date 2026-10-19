package disk

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// Kind is what a folder a rule knows is for.
type Kind int

const (
	KindNone  Kind = iota
	KindDev        // a developer's: dependencies, build output, toolchains
	KindAgent      // coding agents' and AI models'
	KindCache      // programs' caches and the trash
)

// Kinds are the kinds a scan reports, in their order.
var Kinds = []Kind{KindDev, KindAgent, KindCache}

// Name is the kind as the index calls it.
func (k Kind) Name() string {
	switch k {
	case KindDev:
		return i18n.T("Developer")
	case KindAgent:
		return i18n.T("Agents and AI")
	case KindCache:
		return i18n.T("Caches")
	}
	return ""
}

// Tag is the kind as a row's tag shows it.
func (k Kind) Tag() string {
	switch k {
	case KindDev:
		return i18n.T("DEV")
	case KindAgent:
		return i18n.T("AI")
	case KindCache:
		return i18n.T("CACHE")
	}
	return ""
}

// ID is the kind's page in the window and its name on the command line.
func (k Kind) ID() string {
	switch k {
	case KindDev:
		return "dev"
	case KindAgent:
		return "agent"
	case KindCache:
		return "cache"
	}
	return ""
}

// Verdict is whether a folder can go.
type Verdict int

const (
	Unknown Verdict = iota // no rule settles it
	Safe                   // made again, or downloaded again, when needed
	Review                 // the user's to decide: it may hold their work
	Keep                   // deleting it breaks something or loses work
)

// Name is the verdict in words.
func (v Verdict) Name() string {
	switch v {
	case Safe:
		return i18n.T("Safe to delete")
	case Review:
		return i18n.T("Look before deleting")
	case Keep:
		return i18n.T("Keep it")
	}
	return i18n.T("Not decided")
}

// Tag is the verdict as a row's tag shows it.
func (v Verdict) Tag() string {
	switch v {
	case Safe:
		return i18n.T("SAFE")
	case Review:
		return i18n.T("REVIEW")
	case Keep:
		return i18n.T("KEEP")
	}
	return ""
}

// Rule knows a kind of folder: where it is, what made it, and whether it
// can go. Its texts are msgids, translated where they show.
type Rule struct {
	Kind    Kind
	What    string  // what the folder is: "npm packages"
	Verdict Verdict // whether it can go
	Why     string  // why, in a sentence
	Clean   string  // the proper way to clean it, a command; "" for none
	// Terminal: Clean asks before it deletes, or wants more than the
	// folder, so the user runs it in a terminal rather than inua.
	Terminal bool

	// Where: a folder called Name anywhere, or the path Home under the
	// home (a last element * is any folder there), or the absolute Abs.
	Name, Home, Abs string
	// Beside, when given: one of these is in the same folder (a Rust
	// project's Cargo.toml beside target). Inside: one of these is in it.
	Beside, Inside []string
	// In, when given: the name of the folder it is in (.claude for an
	// agent's worktrees).
	In string
	// Group is a folder of an agent or a program whose known parts are
	// listed rather than itself; itself only when none is found.
	Group bool
}

// Rules are the folders inua knows, the more particular first: the first
// that matches a folder is its rule.
var Rules = []*Rule{
	// Coding agents.
	{Kind: KindAgent, Home: ".claude/projects", What: i18n.M("Claude Code conversations"), Verdict: Review,
		Why: i18n.M("Each conversation of Claude Code, by project; deleting them loses what claude --resume continues.")},
	{Kind: KindAgent, Home: ".claude/shell-snapshots", What: i18n.M("Claude Code shell snapshots"), Verdict: Safe,
		Why: i18n.M("Claude Code takes them again when it starts a shell.")},
	{Kind: KindAgent, Home: ".claude/todos", What: i18n.M("Claude Code task lists"), Verdict: Safe,
		Why: i18n.M("The task lists of conversations that ended.")},
	{Kind: KindAgent, Home: ".claude/file-history", What: i18n.M("Claude Code file history"), Verdict: Review,
		Why: i18n.M("Copies of files before Claude Code changed them, for going back.")},
	{Kind: KindAgent, Home: ".claude/debug", What: i18n.M("Claude Code logs"), Verdict: Safe,
		Why: i18n.M("Logs for finding faults; nothing reads them back.")},
	{Kind: KindAgent, Home: ".claude/plugins", What: i18n.M("Claude Code plugins"), Verdict: Review,
		Why: i18n.M("Plugins installed in Claude Code; they are downloaded again when installed again.")},
	{Kind: KindAgent, Home: ".claude", What: i18n.M("Claude Code"), Verdict: Review, Group: true,
		Why: i18n.M("Claude Code's settings, memory and conversations.")},
	{Kind: KindAgent, Home: ".local/share/claude/versions", What: i18n.M("Claude Code versions"), Verdict: Review,
		Why: i18n.M("Each version its updates installed; only the newest is used, so the older ones can go.")},
	{Kind: KindAgent, Name: "worktrees", In: ".claude", What: i18n.M("Agent worktrees"), Verdict: Review,
		Why:   i18n.M("Git worktrees an agent works in; their changes may not be committed yet."),
		Clean: "git worktree remove PATH/NAME"},
	{Kind: KindAgent, Home: ".codex/packages/standalone/releases", What: i18n.M("Codex versions"), Verdict: Review,
		Why: i18n.M("Each version its updates installed; only the newest is used, so the older ones can go.")},
	{Kind: KindAgent, Name: "worktrees", In: ".codex", What: i18n.M("Agent worktrees"), Verdict: Review,
		Why:   i18n.M("Git worktrees an agent works in; their changes may not be committed yet."),
		Clean: "git worktree remove PATH/NAME"},
	{Kind: KindAgent, Home: ".codex/sessions", What: i18n.M("Codex conversations"), Verdict: Review,
		Why: i18n.M("Codex's conversations; deleting them loses what codex resume continues.")},
	{Kind: KindAgent, Home: ".codex/log", What: i18n.M("Codex logs"), Verdict: Safe,
		Why: i18n.M("Logs for finding faults; nothing reads them back.")},
	{Kind: KindAgent, Home: ".codex", What: i18n.M("Codex"), Verdict: Review, Group: true,
		Why: i18n.M("Codex's settings and conversations.")},
	{Kind: KindAgent, Home: ".gemini/tmp", What: i18n.M("Gemini CLI temporary files"), Verdict: Safe,
		Why: i18n.M("Files the Gemini CLI keeps while it works.")},
	{Kind: KindAgent, Home: ".gemini", What: i18n.M("Gemini CLI"), Verdict: Review, Group: true,
		Why: i18n.M("The Gemini CLI's settings and history.")},
	{Kind: KindAgent, Home: ".cursor/extensions", What: i18n.M("Cursor extensions"), Verdict: Safe,
		Why: i18n.M("Cursor downloads its extensions again.")},
	{Kind: KindAgent, Home: ".cursor", What: i18n.M("Cursor"), Verdict: Review, Group: true,
		Why: i18n.M("Cursor's settings and extensions.")},
	{Kind: KindAgent, Home: ".config/Cursor", What: i18n.M("Cursor"), Verdict: Review,
		Why: i18n.M("Cursor's settings, history and caches.")},
	{Kind: KindAgent, Home: ".aider", What: i18n.M("Aider"), Verdict: Review,
		Why: i18n.M("Aider's settings and caches.")},
	{Kind: KindAgent, Home: ".continue", What: i18n.M("Continue"), Verdict: Review,
		Why: i18n.M("Continue's settings, indexes and sessions.")},
	{Kind: KindAgent, Home: ".codeium", What: i18n.M("Windsurf and Codeium"), Verdict: Review,
		Why: i18n.M("Windsurf's and Codeium's settings and language servers.")},
	{Kind: KindAgent, Home: ".local/share/inua/assistant", What: i18n.M("inua's assistant"), Verdict: Review,
		Why: i18n.M("The assistant's memory and tasks.")},
	{Kind: KindAgent, Home: ".npm/_npx", What: i18n.M("npx packages"), Verdict: Safe,
		Why:   i18n.M("Packages npx downloaded to run, such as agents' MCP servers; it downloads them again."),
		Clean: "rm -rf ~/.npm/_npx"},
	// AI models.
	{Kind: KindAgent, Home: ".ollama/models", What: i18n.M("Ollama models"), Verdict: Review,
		Why:   i18n.M("Models Ollama downloaded; delete the ones not used, and pull them again when needed."),
		Clean: "ollama rm MODEL"},
	{Kind: KindAgent, Abs: "/usr/share/ollama/.ollama/models", What: i18n.M("Ollama models"), Verdict: Review,
		Why:   i18n.M("Models Ollama downloaded; delete the ones not used, and pull them again when needed."),
		Clean: "ollama rm MODEL"},
	{Kind: KindAgent, Home: ".cache/huggingface", What: i18n.M("Hugging Face models and datasets"), Verdict: Safe,
		Why:   i18n.M("Downloaded again by the program that asks for them."),
		Clean: "huggingface-cli delete-cache", Terminal: true},
	{Kind: KindAgent, Home: ".cache/torch", What: i18n.M("PyTorch models"), Verdict: Safe,
		Why: i18n.M("Downloaded again by the program that asks for them.")},
	{Kind: KindAgent, Home: ".lmstudio/models", What: i18n.M("LM Studio models"), Verdict: Review,
		Why: i18n.M("Models LM Studio downloaded; it downloads them again when chosen.")},
	{Kind: KindAgent, Home: ".cache/lm-studio", What: i18n.M("LM Studio models"), Verdict: Review,
		Why: i18n.M("Models LM Studio downloaded; it downloads them again when chosen.")},
	{Kind: KindAgent, Home: ".cache/ms-playwright", What: i18n.M("Playwright browsers"), Verdict: Safe,
		Why: i18n.M("Browsers Playwright downloaded for agents and tests; it installs them again.")},
	{Kind: KindAgent, Home: ".cache/puppeteer", What: i18n.M("Puppeteer browsers"), Verdict: Safe,
		Why: i18n.M("Browsers Puppeteer downloaded; it installs them again.")},

	// JavaScript.
	{Kind: KindDev, Name: "node_modules", What: i18n.M("npm packages"), Verdict: Safe,
		Why: i18n.M("The project's packages; installing them again brings them back.")},
	{Kind: KindDev, Home: ".npm", What: i18n.M("npm cache"), Verdict: Safe,
		Why: i18n.M("Packages npm downloaded; it downloads them again."), Clean: "npm cache clean --force"},
	{Kind: KindDev, Home: ".cache/yarn", What: i18n.M("Yarn cache"), Verdict: Safe,
		Why: i18n.M("Packages Yarn downloaded; it downloads them again."), Clean: "yarn cache clean"},
	{Kind: KindDev, Home: ".yarn/berry/cache", What: i18n.M("Yarn cache"), Verdict: Safe,
		Why: i18n.M("Packages Yarn downloaded; it downloads them again."), Clean: "yarn cache clean"},
	{Kind: KindDev, Home: ".local/share/pnpm/store", What: i18n.M("pnpm store"), Verdict: Safe,
		Why: i18n.M("Packages pnpm downloaded; projects link to them, and it downloads them again."), Clean: "pnpm store prune"},
	{Kind: KindDev, Home: ".pnpm-store", What: i18n.M("pnpm store"), Verdict: Safe,
		Why: i18n.M("Packages pnpm downloaded; projects link to them, and it downloads them again."), Clean: "pnpm store prune"},
	{Kind: KindDev, Home: ".bun/install/cache", What: i18n.M("Bun cache"), Verdict: Safe,
		Why: i18n.M("Packages Bun downloaded; it downloads them again."), Clean: "bun pm cache rm"},
	{Kind: KindDev, Home: ".cache/deno", What: i18n.M("Deno cache"), Verdict: Safe,
		Why: i18n.M("Modules Deno downloaded and compiled; it makes them again.")},
	{Kind: KindDev, Name: ".next", Beside: []string{"package.json"}, What: i18n.M("Next.js build"), Verdict: Safe,
		Why: i18n.M("Built from the project's sources; the next build makes it again.")},
	{Kind: KindDev, Name: ".nuxt", Beside: []string{"package.json"}, What: i18n.M("Nuxt build"), Verdict: Safe,
		Why: i18n.M("Built from the project's sources; the next build makes it again.")},
	{Kind: KindDev, Name: ".svelte-kit", Beside: []string{"package.json"}, What: i18n.M("SvelteKit build"), Verdict: Safe,
		Why: i18n.M("Built from the project's sources; the next build makes it again.")},
	{Kind: KindDev, Name: ".turbo", Beside: []string{"package.json", "turbo.json"}, What: i18n.M("Turborepo cache"), Verdict: Safe,
		Why: i18n.M("Build results kept to skip work; the next build makes them again.")},
	{Kind: KindDev, Name: ".parcel-cache", What: i18n.M("Parcel cache"), Verdict: Safe,
		Why: i18n.M("Build results kept to skip work; the next build makes them again.")},

	// Go.
	{Kind: KindDev, Home: ".cache/go-build", What: i18n.M("Go build cache"), Verdict: Safe,
		Why: i18n.M("Compiled packages kept to build faster; go build makes them again."), Clean: "go clean -cache"},
	{Kind: KindDev, Home: "go/pkg/mod", What: i18n.M("Go modules"), Verdict: Safe,
		Why:   i18n.M("Modules Go downloaded; it downloads them again. Go makes them read-only: clean them with go."),
		Clean: "go clean -modcache"},
	{Kind: KindDev, Home: ".cache/golangci-lint", What: i18n.M("golangci-lint cache"), Verdict: Safe,
		Why: i18n.M("Results kept to check faster; the next run makes them again."), Clean: "golangci-lint cache clean"},

	// Rust.
	{Kind: KindDev, Name: "target", Beside: []string{"Cargo.toml"}, What: i18n.M("Rust build"), Verdict: Safe,
		Why: i18n.M("Built from the project's sources; cargo build makes it again."), Clean: "cargo clean"},
	{Kind: KindDev, Home: ".cargo/registry", What: i18n.M("Cargo packages"), Verdict: Safe,
		Why: i18n.M("Crates Cargo downloaded; it downloads them again.")},
	{Kind: KindDev, Home: ".cargo/git", What: i18n.M("Cargo packages"), Verdict: Safe,
		Why: i18n.M("Crates Cargo downloaded; it downloads them again.")},
	{Kind: KindDev, Home: ".cargo/bin", What: i18n.M("Programs installed with Cargo"), Verdict: Review,
		Why:   i18n.M("Programs cargo install built, and rustup's own commands; deleting them uninstalls them."),
		Clean: "cargo uninstall NAME"},
	{Kind: KindDev, Home: ".rustup/toolchains", What: i18n.M("Rust toolchains"), Verdict: Review,
		Why:   i18n.M("The Rust versions installed; remove the ones not used with rustup."),
		Clean: "rustup toolchain uninstall NAME"},
	{Kind: KindDev, Home: ".rustup/downloads", What: i18n.M("rustup downloads"), Verdict: Safe,
		Why: i18n.M("Downloads rustup keeps while it installs; nothing needs them after.")},
	{Kind: KindDev, Home: ".rustup/tmp", What: i18n.M("rustup downloads"), Verdict: Safe,
		Why: i18n.M("Downloads rustup keeps while it installs; nothing needs them after.")},
	{Kind: KindDev, Home: ".cache/sccache", What: i18n.M("sccache cache"), Verdict: Safe,
		Why: i18n.M("Compiled files kept to build faster; the next build makes them again.")},
	{Kind: KindDev, Home: ".cache/rust-analyzer", What: i18n.M("rust-analyzer cache"), Verdict: Safe,
		Why: i18n.M("Indexes rust-analyzer makes again when it opens a project.")},
	{Kind: KindDev, Home: ".cache/cargo-target", What: i18n.M("Rust build"), Verdict: Safe,
		Why: i18n.M("Built from the projects' sources; cargo build makes it again.")},

	// Python.
	{Kind: KindDev, Name: ".venv", Inside: []string{"pyvenv.cfg"}, What: i18n.M("Python environment"), Verdict: Safe,
		Why: i18n.M("The project's packages; installing them again brings them back.")},
	{Kind: KindDev, Name: "venv", Inside: []string{"pyvenv.cfg"}, What: i18n.M("Python environment"), Verdict: Safe,
		Why: i18n.M("The project's packages; installing them again brings them back.")},
	{Kind: KindDev, Name: "__pycache__", What: i18n.M("Python bytecode"), Verdict: Safe,
		Why: i18n.M("Python compiles it again when it runs the code.")},
	{Kind: KindDev, Name: ".pytest_cache", What: i18n.M("pytest cache"), Verdict: Safe,
		Why: i18n.M("Results kept to check faster; the next run makes them again.")},
	{Kind: KindDev, Name: ".mypy_cache", What: i18n.M("mypy cache"), Verdict: Safe,
		Why: i18n.M("Results kept to check faster; the next run makes them again.")},
	{Kind: KindDev, Name: ".ruff_cache", What: i18n.M("Ruff cache"), Verdict: Safe,
		Why: i18n.M("Results kept to check faster; the next run makes them again.")},
	{Kind: KindDev, Name: ".tox", What: i18n.M("tox environments"), Verdict: Safe,
		Why: i18n.M("The project's packages; installing them again brings them back.")},
	{Kind: KindDev, Home: ".cache/pip", What: i18n.M("pip cache"), Verdict: Safe,
		Why: i18n.M("Packages pip downloaded; it downloads them again."), Clean: "pip cache purge"},
	{Kind: KindDev, Home: ".cache/uv", What: i18n.M("uv cache"), Verdict: Safe,
		Why: i18n.M("Packages uv downloaded; it downloads them again."), Clean: "uv cache clean"},
	{Kind: KindDev, Home: ".cache/pypoetry", What: i18n.M("Poetry cache"), Verdict: Safe,
		Why: i18n.M("Packages Poetry downloaded; it downloads them again.")},
	{Kind: KindDev, Home: ".conda/pkgs", What: i18n.M("Conda packages"), Verdict: Safe,
		Why: i18n.M("Packages Conda downloaded; it downloads them again."), Clean: "conda clean --all --yes"},

	// JVM, Android.
	{Kind: KindDev, Home: ".gradle/caches", What: i18n.M("Gradle cache"), Verdict: Safe,
		Why: i18n.M("Packages and build results Gradle keeps; it makes them again.")},
	{Kind: KindDev, Home: ".gradle/wrapper/dists", What: i18n.M("Gradle versions"), Verdict: Safe,
		Why: i18n.M("Gradle versions projects asked for; they are downloaded again.")},
	{Kind: KindDev, Home: ".m2/repository", What: i18n.M("Maven packages"), Verdict: Safe,
		Why: i18n.M("Packages Maven downloaded; it downloads them again.")},
	{Kind: KindDev, Name: "build", Beside: []string{"build.gradle", "build.gradle.kts"}, What: i18n.M("Gradle build"), Verdict: Safe,
		Why: i18n.M("Built from the project's sources; the next build makes it again."), Clean: "./gradlew clean"},
	{Kind: KindDev, Home: ".android/avd", What: i18n.M("Android emulators"), Verdict: Review,
		Why: i18n.M("The emulators' phones, with what was installed on them.")},
	{Kind: KindDev, Home: "Android/Sdk", What: i18n.M("Android SDK"), Verdict: Review,
		Why: i18n.M("Android's tools and system images; the SDK manager installs them again.")},

	// Other toolchains and tools.
	{Kind: KindDev, Name: "zig-cache", What: i18n.M("Zig cache"), Verdict: Safe,
		Why: i18n.M("Build results kept to skip work; the next build makes them again.")},
	{Kind: KindDev, Name: ".zig-cache", What: i18n.M("Zig cache"), Verdict: Safe,
		Why: i18n.M("Build results kept to skip work; the next build makes them again.")},
	{Kind: KindDev, Name: ".gradle", Beside: []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}, What: i18n.M("Gradle cache"), Verdict: Safe,
		Why: i18n.M("Packages and build results Gradle keeps; it makes them again.")},
	{Kind: KindDev, Home: ".cache/ccache", What: i18n.M("ccache"), Verdict: Safe,
		Why: i18n.M("Compiled files kept to build faster; the next build makes them again."), Clean: "ccache -C"},
	{Kind: KindDev, Home: ".cache/bazel", What: i18n.M("Bazel cache"), Verdict: Safe,
		Why: i18n.M("Build results kept to skip work; the next build makes them again."), Clean: "bazel clean --expunge", Terminal: true},
	{Kind: KindDev, Home: ".cache/JetBrains", What: i18n.M("JetBrains caches"), Verdict: Safe,
		Why: i18n.M("Indexes the IDEs make again when they open a project.")},
	{Kind: KindDev, Home: ".vscode/extensions", What: i18n.M("VS Code extensions"), Verdict: Review,
		Why: i18n.M("Extensions installed in VS Code; they are downloaded again when installed again.")},
	{Kind: KindDev, Home: ".vscode-server", What: i18n.M("VS Code server"), Verdict: Safe,
		Why: i18n.M("VS Code installs it again when it connects to this computer.")},
	{Kind: KindDev, Home: ".config/Code/Cache", What: i18n.M("VS Code cache"), Verdict: Safe,
		Why: i18n.M("VS Code makes it again.")},
	{Kind: KindDev, Home: ".config/Code/CachedData", What: i18n.M("VS Code cache"), Verdict: Safe,
		Why: i18n.M("VS Code makes it again.")},
	{Kind: KindDev, Home: ".local/share/containers/storage", What: i18n.M("Podman images and containers"), Verdict: Keep,
		Why:   i18n.M("Podman's images and containers; deleting the folder breaks Podman. Remove them with podman."),
		Clean: "podman system prune", Terminal: true},
	{Kind: KindDev, Abs: "/var/lib/docker", What: i18n.M("Docker images and containers"), Verdict: Keep,
		Why:   i18n.M("Docker's images, containers and volumes; deleting the folder breaks Docker. Remove them with docker."),
		Clean: "docker system prune", Terminal: true},
	{Kind: KindDev, Name: ".git", What: i18n.M("Git history"), Verdict: Keep,
		Why: i18n.M("The project's history, branches and stashes; what is not pushed exists only here.")},

	// Programs' caches.
	{Kind: KindCache, Home: ".local/share/Trash", What: i18n.M("Trash"), Verdict: Safe,
		Why: i18n.M("What was moved to the trash; it takes room until the trash is emptied.")},
	{Kind: KindCache, Abs: "/var/cache/pacman/pkg", What: i18n.M("pacman packages"), Verdict: Safe,
		Why: i18n.M("Packages pacman downloaded; old versions are needed only to go back to them."), Clean: "sudo paccache -r"},
	{Kind: KindCache, Abs: "/var/cache/apt/archives", What: i18n.M("APT packages"), Verdict: Safe,
		Why: i18n.M("Packages APT downloaded and installed already."), Clean: "sudo apt clean"},
	{Kind: KindCache, Abs: "/var/cache/dnf", What: i18n.M("DNF cache"), Verdict: Safe,
		Why: i18n.M("Packages and lists DNF downloaded."), Clean: "sudo dnf clean all"},
	{Kind: KindCache, Abs: "/var/lib/flatpak", What: i18n.M("Flatpak apps"), Verdict: Keep,
		Why:   i18n.M("Installed Flatpak apps and runtimes; remove the ones not used with flatpak."),
		Clean: "flatpak uninstall --unused", Terminal: true},
	{Kind: KindCache, Home: ".local/share/flatpak", What: i18n.M("Flatpak apps"), Verdict: Keep,
		Why:   i18n.M("Installed Flatpak apps and runtimes; remove the ones not used with flatpak."),
		Clean: "flatpak uninstall --unused", Terminal: true},
	{Kind: KindCache, Abs: "/var/log/journal", What: i18n.M("System logs"), Verdict: Keep,
		Why:   i18n.M("The system's journal; shrink it with journalctl rather than deleting files."),
		Clean: "sudo journalctl --vacuum-size=200M"},
	{Kind: KindCache, Home: ".cache/*", What: i18n.M("A program's cache"), Verdict: Safe,
		Why: i18n.M("Programs keep caches to work faster and make them again when they are gone.")},
}

// Folders their program marks with a CACHEDIR.TAG file
// (https://bford.info/cachedir/), when no rule above knows them: Cargo
// marks every target folder so, wherever CARGO_TARGET_DIR puts it.
var (
	rustTarget = &Rule{Kind: KindDev, What: i18n.M("Rust build"), Verdict: Safe,
		Why: i18n.M("Built from the project's sources; cargo build makes it again.")}
	taggedCache = &Rule{Kind: KindCache, What: i18n.M("A cache its program marked"), Verdict: Safe,
		Why: i18n.M("Its program marked it as a cache (CACHEDIR.TAG): one it makes again when it is gone.")}
)

// tagged is the rule for a folder no rule knows that holds a CACHEDIR.TAG.
func tagged(name string) *Rule {
	if name == "target" {
		return rustTarget
	}
	return taggedCache
}

// match is the rule for the folder at path, called name, in a folder
// holding beside; home is the user's home. A rule that wants a file inside
// looks for it.
func match(path, name, home string, beside map[string]bool) *Rule {
	rel := ""
	if home != "" {
		if r, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(r, "..") && r != "." {
			rel = r
		}
	}
	for _, r := range Rules {
		switch {
		case r.Name != "":
			if r.Name != name {
				continue
			}
		case r.Home != "":
			if rel == "" {
				continue
			}
			if dir, ok := strings.CutSuffix(r.Home, "/*"); ok {
				if filepath.Dir(rel) != dir {
					continue
				}
			} else if rel != r.Home {
				continue
			}
		case r.Abs != "":
			if path != r.Abs {
				continue
			}
		default:
			continue
		}
		if r.In != "" && filepath.Base(filepath.Dir(path)) != r.In {
			continue
		}
		if len(r.Beside) > 0 && !anyIn(r.Beside, beside) {
			continue
		}
		if len(r.Inside) > 0 && !anyExists(path, r.Inside) {
			continue
		}
		return r
	}
	return nil
}

// Match is the rule for the folder at path, or nil; home is the user's
// home, "" for the process's.
func Match(path, home string) *Rule {
	path = filepath.Clean(path)
	beside := map[string]bool{}
	if entries, err := os.ReadDir(filepath.Dir(path)); err == nil {
		for _, e := range entries {
			beside[e.Name()] = true
		}
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if r := match(path, filepath.Base(path), home, beside); r != nil {
		return r
	}
	if anyExists(path, []string{"CACHEDIR.TAG"}) {
		return tagged(filepath.Base(path))
	}
	return nil
}

func anyIn(names []string, set map[string]bool) bool {
	for _, n := range names {
		if set[n] {
			return true
		}
	}
	return false
}

func anyExists(dir string, names []string) bool {
	for _, n := range names {
		if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}
