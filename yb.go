// Copyright 2024 Tamas Gulacsi. All rights reserved.
//
// SPDX-License-Identifier: Apache-2.0

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
	"time"

	"github.com/goyek/goyek/v2"
	"github.com/goyek/x/boot"
)

type (
	A           = goyek.A
	DefinedTask = goyek.DefinedTask
	Deps        = goyek.Deps
	Task        = goyek.Task
)

var (
	Define     = goyek.Define
	Main       = boot.Main
	SetDefault = goyek.SetDefault
)

func GoDeps(ctx context.Context, name string) []string {
	b, _ := exec.CommandContext(ctx, "go", "list", "./"+name).Output()
	prefix := string(bytes.TrimSpace(b))
	cmd := exec.CommandContext(ctx, "go", "list", "-f", "{{range .Deps}}{{.}}\n{{end}}", "./"+name)
	b, err := cmd.Output()
	if err != nil {
		slog.Error("goDeps", "cmd", cmd.Args, "out", string(b), "error", err)
	}
	lines := bytes.Split(b, []byte("\n"))
	deps := make([]string, 0, len(lines))
	for _, b := range lines {
		if b, ok := bytes.CutPrefix(b, []byte(prefix)); ok {
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
	if old, err := QtcIsOld(name); err != nil || old {
		return true
	}
	destTime := MTime(filepath.Join(GoBin, name))
	if destTime == 0 {
		nm, _ := PackageName("./" + name)
		return nm == "main"
	}
	goModTime := MTime("go.mod")
	if destTime != 0 && destTime < goModTime {
		return true
	}
	files, _ := filepath.Glob(filepath.Join(name, "*.go"))
	maxTime := MTime(files...)
	// a.Logf("destTime=%d goModTime=%d maxTime=%d", destTime, goModTime, maxTime)
	return maxTime < goModTime || destTime != 0 && destTime < maxTime
}

func QtcIsOld(root string) (bool, error) {
	var old bool
	err := filepath.WalkDir(root, func(path string, di fs.DirEntry, err error) error {
		if err != nil {
			slog.Error("walk", "path", path, "error", err)
			return nil
		}
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

func PackageName(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	b, err := exec.CommandContext(ctx, "go", "list", "-f", "{{.Name}}", path).Output()
	cancel()
	return string(bytes.TrimSpace(b)), err
}
