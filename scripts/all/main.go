package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

func main() {
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	gamePath := flag.String("game", catalog.DefaultGamePath, "Darkspore install root")
	skipAssets := flag.Bool("skip-assets", false, "skip package image extraction")
	flag.Parse()
	tools := [][]string{
		{"run", "./scripts/hero", "-db", *databasePath},
		{"run", "./scripts/ability", "-db", *databasePath, "-game", *gamePath},
		{"run", "./scripts/buff", "-db", *databasePath},
		{"run", "./scripts/effect", "-db", *databasePath, "-game", *gamePath},
		{"run", "./scripts/glossary"},
		{"run", "./scripts/npc", "-db", *databasePath},
		{"run", "./scripts/level", "-db", *databasePath},
		{"run", "./scripts/item"},
		{"run", "./scripts/combat"},
		{"run", "./scripts/search"},
	}
	if !*skipAssets {
		tools = append(tools, []string{"run", "./scripts/assets", "-game", *gamePath})
	}
	for _, arguments := range tools {
		err := run(context.Background(), arguments)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("empty go arguments")
	}
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if err != nil {
		return fmt.Errorf("goRun[%s]: %w", arguments[1], err)
	}
	return nil
}
