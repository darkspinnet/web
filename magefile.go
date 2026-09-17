//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"
)

// Default builds the production website from the current generated inputs.
var Default = Build

// Hero refreshes the playable hero JSON catalog.
func Hero() error {
	err := run("go", "run", "./scripts/hero")
	if err != nil {
		return fmt.Errorf("heroes: %w", err)
	}
	return nil
}

// Ability refreshes the hero ability ownership catalog and global lookup index.
func Ability() error {
	if err := generateAbility(); err != nil {
		return err
	}
	if err := Search(); err != nil {
		return fmt.Errorf("abilitySearch: %w", err)
	}
	return nil
}

func generateAbility() error {
	if err := run("go", "run", "./scripts/ability"); err != nil {
		return fmt.Errorf("abilities: %w", err)
	}
	return nil
}

// Buff refreshes localized gameplay conditions and their ability links.
func Buff() error {
	if err := generateBuff(); err != nil {
		return err
	}
	if err := Search(); err != nil {
		return fmt.Errorf("buffSearch: %w", err)
	}
	return nil
}

func generateBuff() error {
	if err := run("go", "run", "./scripts/buff"); err != nil {
		return fmt.Errorf("buff: %w", err)
	}
	return nil
}

// Effect refreshes the packaged and Lua-referenced visual-effect index.
func Effect() error {
	if err := generateEffect(); err != nil {
		return err
	}
	if err := Search(); err != nil {
		return fmt.Errorf("effectSearch: %w", err)
	}
	return nil
}

func generateEffect() error {
	if err := run("go", "run", "./scripts/effect"); err != nil {
		return fmt.Errorf("effect: %w", err)
	}
	return nil
}

// Glossary refreshes the general terminology catalog and lookup index.
func Glossary() error {
	if err := generateGlossary(); err != nil {
		return err
	}
	if err := Search(); err != nil {
		return fmt.Errorf("glossarySearch: %w", err)
	}
	return nil
}

func generateGlossary() error {
	if err := run("go", "run", "./scripts/glossary"); err != nil {
		return fmt.Errorf("glossary: %w", err)
	}
	return nil
}

// Npc refreshes the level-grouped non-player entity JSON catalog.
func Npc() error {
	err := run("go", "run", "./scripts/npc")
	if err != nil {
		return fmt.Errorf("npcs: %w", err)
	}
	return nil
}

// Level refreshes the decoded level report JSON catalog.
func Level() error {
	err := run("go", "run", "./scripts/level")
	if err != nil {
		return fmt.Errorf("levels: %w", err)
	}
	return nil
}

// Item refreshes the item catalog scaffold used by the archive and search index.
func Item() error {
	if err := generateItem(); err != nil {
		return err
	}
	if err := Search(); err != nil {
		return fmt.Errorf("itemSearch: %w", err)
	}
	return nil
}

func generateItem() error {
	err := run("go", "run", "./scripts/item")
	if err != nil {
		return fmt.Errorf("items: %w", err)
	}
	return nil
}

// System refreshes source-derived server drop, progression, and reward reports.
func System() error {
	if err := generateSystem(); err != nil {
		return err
	}
	if err := Search(); err != nil {
		return fmt.Errorf("systemSearch: %w", err)
	}
	return nil
}

func generateSystem() error {
	err := run("go", "run", "./scripts/system")
	if err != nil {
		return fmt.Errorf("systems: %w", err)
	}
	return nil
}

// Search refreshes the compact global lookup index.
func Search() error {
	err := run("go", "run", "./scripts/search")
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	return nil
}

// Asset extracts canonical hero portraits plus item, ability, and verified buff icons, then converts them to WebP.
func Asset() error {
	err := run("go", "run", "./scripts/assets")
	if err != nil {
		return fmt.Errorf("assets: %w", err)
	}
	return nil
}

// Generate refreshes every generated JSON catalog and image asset.
func Generate() {
	mg.SerialDeps(Hero, generateAbility, generateBuff, generateEffect, generateGlossary, Npc, Level, generateItem, generateSystem, Search, Asset)
}

// Build writes the Hugo site to public using current generated inputs.
func Build() error {
	err := run("hugo", "--cleanDestinationDir")
	if err != nil {
		return fmt.Errorf("hugoBuild: %w", err)
	}
	return nil
}

// All refreshes every generated input and then builds the Hugo site.
func All() {
	mg.SerialDeps(Hero, generateAbility, generateBuff, generateEffect, generateGlossary, Npc, Level, generateItem, generateSystem, Search, Asset, Build)
}

// Server starts the Hugo development server using the current generated data.
func Server() error {
	err := run("hugo", "server", "--disableFastRender")
	if err != nil {
		return fmt.Errorf("hugoServer: %w", err)
	}
	return nil
}

func run(name string, arguments ...string) error {
	command := exec.Command(name, arguments...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if err != nil {
		return fmt.Errorf("command[%s]: %w", name, err)
	}
	return nil
}
