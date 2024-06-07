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
	ctx := ContextWithA(a)
	if old, err := QtcIsOld(ctx, a.Name()); err != nil {
		a.Error(err)
	} else if old {
		Run(a, []string{"qtc"}, AtDir(a.Name()))
	}
	if GoShouldBuild(ctx, a.Name()) {
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

func GoShouldBuild(ctx context.Context, name string) bool {
	logger := LoggerFromContext(ctx)
	if old, err := QtcIsOld(ctx, name); err != nil {
		logger.Error("QtcIsOld", "error", err)
		return true
	} else if old {
		logger.Log("QtcIsOld")
		return true
	}
	destTime := MTime(filepath.Join(GoBin, name))
	if destTime == 0 {
		nm, _ := PackageName("./" + name)
		logger.Log("no dest", "name", nm)
		if nm == "main" {
			return true
		}
	}
	goModTime := MTime("go.mod")
	if destTime != 0 && destTime < goModTime {
		logger.Log("go.mod is newer")
		return true
	}
	files, _ := filepath.Glob(filepath.Join(name, "*.go"))
	maxTime := MTime(files...)
	if destTime != 0 && destTime < maxTime {
		logger.Log("*.go is newer than dest")
	}
	return false
}

func QtcIsOld(ctx context.Context, root string) (bool, error) {
	logger := LoggerFromContext(ctx)
	var old bool
	err := filepath.WalkDir(root, func(path string, di fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			logger.Error("walk", "path", path, "error", err)
			return nil
		}
		if old {
			return fs.SkipAll
		}
		if di.Type().IsRegular() && strings.HasSuffix(path, ".qtpl") {
			fi, err := di.Info()
			if err != nil {
				logger.Error("stat", "file", di.Name(), "error", err)
				old = true
				return err
			}
			if old = fi.ModTime().UnixMilli() > MTime(path+".go"); old {
				logger.Log("go is older than qtc", "path", path)
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

type ctxA struct{}

func ContextWithA(a *A) context.Context {
	return context.WithValue(a.Context(), ctxA{}, a)
}
func AFromContext(ctx context.Context) *A { a, _ := ctx.Value(ctxA{}).(*A); return a }

type aLogger interface {
	Log(...any)
	Error(...any)
}

type ctxLog struct{}

func ContextWithLogger(ctx context.Context, logger aLogger) context.Context {
	return context.WithValue(ctx, ctxLog{}, logger)
}
func LoggerFromContext(ctx context.Context) aLogger {
	if logger, ok := ctx.Value(ctxLog{}).(aLogger); ok {
		return logger
	}
	return defaultLogger{slog.Default()}
}

type defaultLogger struct{ *slog.Logger }

func (lgr defaultLogger) Log(args ...any) {
	if len(args) == 0 {
		return
	}
	s, ok := args[0].(string)
	if ok {
		args = args[1:]
	} else {
		s = "info"
	}
	lgr.Logger.Info(s, args...)
}
func (lgr defaultLogger) Error(args ...any) {
	if len(args) == 0 {
		return
	}
	s, ok := args[0].(string)
	if ok {
		args = args[1:]
	} else {
		s = "ERROR"
	}
	lgr.Logger.Error(s, args...)
}
