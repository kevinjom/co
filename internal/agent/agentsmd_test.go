package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/llm/openai"
	"github.com/kevinjom/co/internal/skill"
)

func TestLoadAgentsmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, root string) string
		want  string
	}{
		{
			name: "closest file in the working directory",
			setup: func(t *testing.T, root string) string {
				repo := filepath.Join(root, "repo")
				pkg := filepath.Join(repo, "pkg")
				mkdirGit(t, repo)
				writeAgentsmd(t, filepath.Join(repo, agentsmdFile), "from the root")
				writeAgentsmd(t, filepath.Join(pkg, agentsmdFile), "from the package")
				return pkg
			},
			want: agentsmdBlock("from the package", agentsmdFile),
		},
		{
			name: "parent file when the working directory has none",
			setup: func(t *testing.T, root string) string {
				repo := filepath.Join(root, "repo")
				pkg := filepath.Join(repo, "pkg")
				mkdirGit(t, repo)
				writeAgentsmd(t, filepath.Join(repo, agentsmdFile), "from the root")
				if err := os.MkdirAll(pkg, 0o755); err != nil {
					t.Fatal(err)
				}
				return pkg
			},
			want: agentsmdBlock("from the root", "../"+agentsmdFile),
		},
		{
			name: "file above the git root is ignored",
			setup: func(t *testing.T, root string) string {
				repo := filepath.Join(root, "repo")
				pkg := filepath.Join(repo, "pkg")
				mkdirGit(t, repo)
				writeAgentsmd(t, filepath.Join(root, agentsmdFile), "outside the repo")
				if err := os.MkdirAll(pkg, 0o755); err != nil {
					t.Fatal(err)
				}
				return pkg
			},
		},
		{
			name: "without a git repo only the working directory is read",
			setup: func(t *testing.T, root string) string {
				sub := filepath.Join(root, "sub")
				writeAgentsmd(t, filepath.Join(root, agentsmdFile), "parent")
				writeAgentsmd(t, filepath.Join(sub, agentsmdFile), "here")
				return sub
			},
			want: agentsmdBlock("here", agentsmdFile),
		},
		{
			name: "without a git repo a parent file is ignored",
			setup: func(t *testing.T, root string) string {
				sub := filepath.Join(root, "sub")
				writeAgentsmd(t, filepath.Join(root, agentsmdFile), "parent")
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatal(err)
				}
				return sub
			},
		},
		{
			name: "empty file hides the parent",
			setup: func(t *testing.T, root string) string {
				repo := filepath.Join(root, "repo")
				pkg := filepath.Join(repo, "pkg")
				mkdirGit(t, repo)
				writeAgentsmd(t, filepath.Join(repo, agentsmdFile), "from the root")
				writeAgentsmd(t, filepath.Join(pkg, agentsmdFile), " \n\t")
				return pkg
			},
		},
		{
			name: "non-text file hides the parent",
			setup: func(t *testing.T, root string) string {
				repo := filepath.Join(root, "repo")
				pkg := filepath.Join(repo, "pkg")
				mkdirGit(t, repo)
				writeAgentsmd(t, filepath.Join(repo, agentsmdFile), "from the root")
				writeAgentsmdBytes(t, filepath.Join(pkg, agentsmdFile), []byte{0xff, 0xfe, 0x00})
				return pkg
			},
		},
		{
			name: "directory named AGENTS.md is skipped",
			setup: func(t *testing.T, root string) string {
				repo := filepath.Join(root, "repo")
				pkg := filepath.Join(repo, "pkg")
				mkdirGit(t, repo)
				writeAgentsmd(t, filepath.Join(repo, agentsmdFile), "from the root")
				if err := os.MkdirAll(filepath.Join(pkg, agentsmdFile), 0o755); err != nil {
					t.Fatal(err)
				}
				return pkg
			},
			want: agentsmdBlock("from the root", "../"+agentsmdFile),
		},
		{
			name: "git file marks the root",
			setup: func(t *testing.T, root string) string {
				repo := filepath.Join(root, "repo")
				pkg := filepath.Join(repo, "pkg")
				if err := os.MkdirAll(repo, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				writeAgentsmd(t, filepath.Join(root, agentsmdFile), "outside the repo")
				writeAgentsmd(t, filepath.Join(repo, agentsmdFile), "from the root")
				if err := os.MkdirAll(pkg, 0o755); err != nil {
					t.Fatal(err)
				}
				return pkg
			},
			want: agentsmdBlock("from the root", "../"+agentsmdFile),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cwd := tt.setup(t, t.TempDir())
			got := loadAgentsmd(cwd)
			if got != tt.want {
				t.Fatalf("loadAgentsmd() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionKeepsAgentsmd(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkdirGit(t, repo)
	writeAgentsmd(t, filepath.Join(repo, agentsmdFile), "run go test")
	writeAgentSkill(t, repo, "grilling", "Grill a plan.", "Body")

	session := NewSession(openai.New("http://127.0.0.1:1"), "m", repo, nil)
	session.SetCatalog(skill.Load(repo, t.TempDir()))
	session.EnableLSP(idleLanguage{})

	prompt := session.Messages()[0].Content
	want := agentsmdBlock("run go test", agentsmdFile)
	if !strings.Contains(prompt, want) {
		t.Fatalf("prompt missing project instructions\n%s", prompt)
	}
	rules := strings.Index(prompt, "Do not use bash to create or edit source files.")
	project := strings.Index(prompt, "Project instructions from "+agentsmdFile+":")
	skillLine := strings.Index(prompt, "grilling: Grill a plan.")
	if rules < 0 || project < 0 || skillLine < 0 || rules > project || skillLine > project {
		t.Fatalf("prompt order rules=%d skill=%d project=%d\n%s", rules, skillLine, project, prompt)
	}
}

func mkdirGit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeAgentsmd(t *testing.T, path, body string) {
	t.Helper()
	writeAgentsmdBytes(t, path, []byte(body))
}

func writeAgentsmdBytes(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
