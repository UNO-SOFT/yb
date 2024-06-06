// Copyright 2024 Tamas Gulacsi. All rights reserved.

package yb

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goyek/goyek/v2"
	"github.com/goyek/x/boot"
)

type (
	A           = goyek.A
	DefinedTask = goyek.DefinedTask
	Deps        = goyek.Deps
	Task = goyek.Task
)

var (
	Define = goyek.Define
	Main       = boot.Main
	SetDefault = goyek.SetDefault
)

func GoDeps(ctx context.Context, name string) []string {
	cmd := exec.CommandContext(ctx, "go", "list", "-f", "{{range .Deps}}{{.}}\n{{end}}", "./"+name)
	b, err := cmd.Output()
	if err != nil {
		slog.Error("goDeps", "cmd", cmd.Args, "out", string(b), "error", err)
	}
	lines := bytes.Split(b, []byte("\n"))
	deps := make([]string, 0, len(lines))
	for _, b := range lines {
		if b, ok := bytes.CutPrefix(b, []byte("unosoft.hu/sysutils/")); ok {
			deps = append(deps, string(b))
		}
	}
	return deps
}

var (
	brunoCus = os.Getenv("BRUNO_CUS")

	_goBin, _ = exec.CommandContext(context.Background(), "go", "env", "GOBIN").Output()
	GoBin     = string(bytes.TrimSpace(_goBin))
)

func GoInstall(a *goyek.A, force bool) bool {
	a.Helper()
	if old, err := QtcIsOld(a.Name()); err != nil {
		a.Error(err)
	} else if old {
		Run(a, []string{"qtc"}, AtDir(a.Name()))
	}
	if GoShouldBuild(a.Name()) {
		Run(a, []string{"go", "install", "-ldflags=-s -w", "-tags=" + brunoCus, "./" + a.Name()})
		return true
	}
	return false
}

func MTime(paths ...string) int64 {
	var maxTime int64
	for _, fn := range paths {
		fi, err := os.Stat(fn)
		if err != nil {
			continue
		}
		if i := fi.ModTime().UnixMilli(); i > maxTime {
			maxTime = i
		}
	}
	return maxTime
}

func GoShouldBuild(name string) bool {
	if old, err := QtcIsOld(name); err != nil {
		return true
	} else if old {
		return true
	}
	goModTime := MTime("go.mod")
	destTime := MTime(filepath.Join(GoBin, name))
	if destTime != 0 && destTime < goModTime {
		return true
	}
	files, _ := filepath.Glob(filepath.Join(name, "*.go"))
	maxTime := MTime(files...)
	return maxTime < goModTime || destTime != 0 && destTime < goModTime
}

func QtcIsOld(root string) (bool, error) {
	var old bool
	err := filepath.WalkDir(root, func(path string, di fs.DirEntry, err error) error {
		if old {
			return fs.SkipAll
		}
		if di.Type().IsRegular() && strings.HasSuffix(path, ".qtpl") {
			fi, err := di.Info()
			if err != nil {
				old = true
				return err
			}
			if old = fi.ModTime().UnixMilli() > MTime(path+".go"); old {
				return fs.SkipAll
			}
		}
		return nil
	})
	return old, err
}

func Run(a *goyek.A, progArgs []string, runOptions ...runOption) {
	cmd := exec.CommandContext(a.Context(), progArgs[0], progArgs[1:]...)
	for _, o := range runOptions {
		o(cmd)
	}
	a.Logf("%q", cmd.Args)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		a.Errorf("%q: %w", cmd.Args, err)
	}
}

type runOption func(*exec.Cmd)

func AtDir(dir string) runOption { return func(cmd *exec.Cmd) { cmd.Dir = dir } }

func ReadDirLinks(path string) ([]string, error) {
	dis, err := os.ReadDir(path)
	var links []string
	for _, di := range dis {
		links = append(links, di.Name())
	}
	return links, err
}