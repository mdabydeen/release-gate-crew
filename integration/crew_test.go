//go:build integration

// Package integration runs the real docker-agent runtime against both crew
// configs, with the scripted stub standing in for the model. It checks the
// guardrail requirements in SPEC.md (R1-R9). It needs the docker-agent
// binary: set DOCKER_AGENT, or have docker-agent or `docker agent` available.
//
//	go test -tags integration ./integration/...
package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdabydeen/release-gate-crew/internal/stub"
)

// stubAddr must match base_url in the "offline" flavor of both configs.
const stubAddr = "127.0.0.1:8089"

const question = "Is target/ ready to ship?"

func TestMain(m *testing.M) {
	ln, err := net.Listen("tcp", stubAddr)
	if err != nil {
		panic("stub model: " + err.Error())
	}
	srv := &http.Server{Handler: &stub.Server{}, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	code := m.Run()
	_ = srv.Close()
	os.Exit(code)
}

func agentCmd(t *testing.T) []string {
	t.Helper()
	if p := os.Getenv("DOCKER_AGENT"); p != "" {
		return []string{p}
	}
	if p, err := exec.LookPath("docker-agent"); err == nil {
		return []string{p}
	}
	if p, err := exec.LookPath("docker"); err == nil {
		if exec.Command(p, "agent", "version").Run() == nil {
			return []string{p, "agent"}
		}
	}
	t.Skip("docker-agent not found; set DOCKER_AGENT")
	return nil
}

// workspace copies the crew into a temp dir so runs never touch the checkout.
func workspace(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	for _, p := range []string{"crew.yaml", "crew.hcl", ".agentsignore", "prompts", "target", "reports"} {
		if err := copyTree(filepath.Join(root, p), filepath.Join(dst, p)); err != nil {
			t.Fatalf("copy %s: %v", p, err)
		}
	}
	return dst
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := agentCmd(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, cmd[0], append(cmd[1:], args...)...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"TELEMETRY_ENABLED=false",
		"STUB_API_KEY=stub",
		"ANTHROPIC_API_KEY=unused-offline",
		"HOME="+t.TempDir(), // isolate user config, sessions, and global permissions
	)
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	if err := c.Run(); err != nil {
		t.Fatalf("%v %v: %v\n%s", cmd, args, err, out.String())
	}
	return out.String()
}

func TestCrew(t *testing.T) {
	for _, cfg := range []string{"crew.yaml", "crew.hcl"} {
		t.Run(cfg, func(t *testing.T) {
			dir := workspace(t)
			original, err := os.ReadFile(filepath.Join(dir, "target/Dockerfile"))
			if err != nil {
				t.Fatal(err)
			}

			t.Run("R1 config loads", func(t *testing.T) {
				run(t, dir, "run", cfg, "--dry-run")
			})

			out := run(t, dir, "run", cfg, "--flavor", "offline", "--exec", question)

			checks := []struct{ id, want string }{
				{"R2 delegates to container_auditor", `agent: "container_auditor"`},
				{"R2 delegates to pipeline_auditor", `agent: "pipeline_auditor"`},
				{"R2 delegates to scribe", `agent: "scribe"`},
				{"R3 allowed grep runs", "target/Dockerfile:2:FROM node:latest"},
				{"R4 .agentsignore hides target/.env", "excluded by .agentsignore"},
				{"R5 auditor read scope holds", "outside the allowed directories (./target)"},
				{"R6 compound shell is denied", "Tool 'shell' is denied by permissions"},
				{"R7 scribe cannot write target/", "Tool 'write_file' is denied"},
			}
			for _, c := range checks {
				t.Run(c.id, func(t *testing.T) {
					if !strings.Contains(out, c.want) {
						t.Fatalf("output missing %q\n%s", c.want, out)
					}
				})
			}

			t.Run("R8 report written with findings", func(t *testing.T) {
				b, err := os.ReadFile(filepath.Join(dir, "reports/gate-report.md"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(b), "FROM node:latest") {
					t.Fatalf("report lacks forwarded findings:\n%s", b)
				}
			})

			t.Run("R9 target unchanged", func(t *testing.T) {
				now, err := os.ReadFile(filepath.Join(dir, "target/Dockerfile"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(now, original) {
					t.Fatal("target/Dockerfile was modified")
				}
			})

			// Documents the footgun in docs/FINDINGS.md. If docker-agent ever
			// starts splitting compound commands, this test fails and the
			// finding should be revisited.
			t.Run("F1 prefix globs let a denied binary through", func(t *testing.T) {
				out := run(t, dir, "run", cfg, "--flavor", "offline", "--flavor", "naive-permissions", "--exec", question)
				if !strings.Contains(out, "Protocols:") {
					t.Fatalf("expected curl --version banner (footgun reproduced); got:\n%s", out)
				}
			})
		})
	}
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, fi.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
