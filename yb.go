// Copyright 2024 Tamas Gulacsi. All rights reserved.
//
// SPDX-License-Identifier: Apache-2.0

// Package yb contains some helpers and wraps goyek/goyek/v2.
package yb

import (
	"bytes"
	"context"
	"fmt"
	"go/build"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

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

	installedMu sync.RWMutex
	installed   = make(map[string]struct{})
)

// GoDeps returns the subpackage names that the given package depends on.
func GoDeps(ctx context.Context, name string) []string {
	pkg, err := build.ImportDir("./"+name, build.IgnoreVendor)
	if err != nil {
		panic(err)
	}
	prefix := pkg.Name
	deps := make([]string, 0, len(pkg.Imports))
	for _, s := range pkg.Imports {
		if s, ok := strings.CutPrefix(s, prefix); ok {
			deps = append(deps, s)
		}
	}
	return deps
}

var (
	brunoCus = os.Getenv("BRUNO_CUS")

	GoBin = os.Getenv("GOBIN")
)

func init() {
	if GoBin == "" {
		b, _ := exec.CommandContext(context.Background(), "go", "env", "GOBIN").Output()
		GoBin = string(bytes.TrimSpace(b))
	}
}

// Installed returns the list of succeessfully installed names.
func Installed() []string {
	installedMu.RLock()
	defer installedMu.RUnlock()
	ss := make([]string, 0, len(installed))
	for k := range installed {
		ss = append(ss, k)
	}
	return ss
}

// ResetInstalled empties the installed set.
func ResetInstalled() { installedMu.Lock(); clear(installed); installedMu.Unlock() }

// GoInstall go install the given name.
func GoInstall(ctx context.Context, name string, force bool) (bool, error) {
	if old, err := QtcIsOld(ctx, name); err != nil {
		return true, err
	} else if old || force {
		cmd := exec.CommandContext(ctx, "qtc")
		cmd.Dir = name
		if b, err := cmd.CombinedOutput(); err != nil {
			return true, fmt.Errorf("%s: %w", string(b), err)
		}
	}
	if force || GoShouldBuild(ctx, name) {
		cmd := exec.CommandContext(ctx, "go", "install", "-ldflags=-s -w", "-tags="+brunoCus, "./"+name)
		if b, err := cmd.CombinedOutput(); err != nil {
			return true, fmt.Errorf("%s: %w", string(b), err)
		}
		installedMu.Lock()
		installed[name] = struct{}{}
		installedMu.Unlock()
		return true, nil
	}
	return false, nil

}

// GoInstallA is GoInstall for a.Name() with a.Context().
func GoInstallA(a *goyek.A, force bool) {
	a.Helper()
	if _, err := GoInstall(ContextWithA(a), a.Name(), force); err != nil {
		a.Fatal(err)
	}
}

// MTime returns the maximum ModTime().UnixMillis() of the given paths.
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

// GoShouldBuild reports whether the given package needs compilation.
func GoShouldBuild(ctx context.Context, name string) bool {
	logger := LoggerFromContext(ctx)
	if old, err := QtcIsOld(ctx, name); err != nil {
		logger.Error("QtcIsOld", "error", err)
		return true
	} else if old {
		logger.Log("QtcIsOld")
		return true
	}
	var pkg *build.Package

	destTime := MTime(filepath.Join(GoBin, name))
	if destTime == 0 {
		if pkg == nil {
			pkg, _ = build.ImportDir("./"+name, build.IgnoreVendor)
		}
		if pkg.IsCommand() {
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

// QtcIsOld reports whether the given directory needs qtc to be run.
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

// Run an external program reporting on a.
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

// AtDir runOption sets cmd.Dir.
func AtDir(dir string) runOption { return func(cmd *exec.Cmd) { cmd.Dir = dir } }

// ReadDirLinks reads the links contained at path dir.
func ReadDirLinks(path string) ([]string, error) {
	dis, err := os.ReadDir(path)
	var links []string
	for _, di := range dis {
		links = append(links, di.Name())
	}
	return links, err
}

// PackageName returns the name of the package at the given path.
func PackageName(path string) (string, error) {
	pkg, err := build.ImportDir(path, build.IgnoreVendor)
	if err != nil {
		return "", err
	}
	return pkg.Name, nil
}

type ctxA struct{}

// ContextWithA returns a.Context()
func ContextWithA(a *A) context.Context {
	return context.WithValue(a.Context(), ctxA{}, a)
}

// AFromContext returns A from the Context.
func AFromContext(ctx context.Context) *A { a, _ := ctx.Value(ctxA{}).(*A); return a }

type aLogger interface {
	Log(...any)
	Error(...any)
}

type ctxLog struct{}

// ComtextWithLogger returns a context with the logger set.
func ContextWithLogger(ctx context.Context, logger aLogger) context.Context {
	return context.WithValue(ctx, ctxLog{}, logger)
}

// LoggerFromContext returns the logger from the context, or a default slog logger.
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
	lgr.Logger.Debug(s, args...)
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
