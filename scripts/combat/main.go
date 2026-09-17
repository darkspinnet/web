package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

func main() {
	serverPath := flag.String("server", filepath.Join("..", "darkspin"), "Darkspin source repository")
	outputPath := flag.String("out", filepath.Join("data", "generated", "combat.json"), "generated combat report")
	flag.Parse()
	report, err := buildCombat(*serverPath)
	if err == nil {
		err = catalog.WriteJSON(*outputPath, report)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("combat: wrote revision %s to %s\n", report.ShortRevision, *outputPath)
}

// Review the report's prose, equations, and examples before advancing this
// baseline. Cap-only tuning can be regenerated without changing the baseline;
// changed decision functions require another source review.
const reviewedCombatRevision = "b03fc3e007eec7a0442895ab98094e76f53d5a7c"
const darkspinSourceURL = "https://github.com/darkspinnet/darkspin"

type combatSourceSpec struct {
	Name      string
	Path      string
	Functions []string
}

var combatSourceSpecs = []combatSourceSpec{
	{"Hero avoidance and mitigation", "server/combat/defense.go", []string{
		"RollAvoidance", "ReduceIncomingDamage", "ApplyDamageReduction", "defenseRemaining",
	}},
	{"NPC and companion defenses", "server/zone/npc/defense.go", []string{
		"ConfigureDefense", "reduceDamage", "npcDefenseRemaining", "ReduceCompanionDamage",
	}},
	{"Equipment and damage classification", "server/gameplay/defense.go", []string{
		"equipmentDefense", "enemyDefenseRequest", "rollDefense", "reduceCompanionDamage", "isTargetDebuffImmuneLocked",
	}},
	{"NPC damage, vulnerability, and absorption", "server/zone/npc/session.go", []string{"damage"}},
	{"Combat hits and scripted defeat", "server/zone/npc/damage.go", []string{"Hit", "Defeat"}},
	{"Passive and aura reductions", "server/gameplay/passive.go", []string{
		"applyPassiveDamageReduction", "applyCrushingDreadReduction", "crushingDreadReduction",
	}},
	{"PvP shields and damage", "server/gameplay/arena.go", []string{"handleArenaCharacter"}},
	{"Soul Link and hit reactions", "server/gameplay/combat.go", []string{
		"applyEnemyDamage", "commitHeroTargetDamage", "commitHeroTargetReactions",
	}},
	{"Thorn Bark reflection", "server/gameplay/passive_thorn.go", []string{
		"applyEnemyAttackDamage", "commitThornBarkReflection", "publishThornBarkReflection",
	}},
	{"Enemy life drain", "server/gameplay/combat_drain.go", []string{"tick"}},
	{"Catalyst combat bonuses", "server/gameplay/session.go", []string{"setCrystalInventory"}},
	{"Damage and defense-based attack bonuses", "server/game/damage.go", []string{"damageProfile"}},
	{"Expunge's remaining-damage burst", "server/gameplay/ability_expunge.go", []string{"expunge"}},
	{"Area and single-target damage", "server/zone/ability/area.go", []string{"CommitArea"}},
	{"Sprout poison classification", "server/zone/ability/cloud.go", []string{"PlanCloudLob"}},
}

type combatSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type combatCommit struct {
	Revision string `json:"revision"`
	Date     string `json:"date"`
	Summary  string `json:"summary"`
	URL      string `json:"url"`
}

type combatRatingSample struct {
	RatingRatio   float64 `json:"rating_ratio"`
	HeroAvoidance float64 `json:"hero_avoidance"`
	NPCReduction  float64 `json:"npc_reduction"`
}

type combatHitSample struct {
	Name      string  `json:"name"`
	Incoming  float64 `json:"incoming"`
	BeforeCap float64 `json:"before_cap"`
	Floor     float64 `json:"floor"`
	Damage    float64 `json:"damage"`
}

type combatCatalog struct {
	Revision       string               `json:"revision"`
	ShortRevision  string               `json:"short_revision"`
	RevisionDate   string               `json:"revision_date"`
	RevisionURL    string               `json:"revision_url"`
	HeroAvoidance  float64              `json:"hero_avoidance"`
	HeroMitigation float64              `json:"hero_mitigation"`
	NPCPassive     float64              `json:"npc_passive"`
	NPCPhase       float64              `json:"npc_phase"`
	RatingSamples  []combatRatingSample `json:"rating_samples"`
	HitSamples     []combatHitSample    `json:"hit_samples"`
	HeroCurve      string               `json:"hero_curve"`
	NPCCurve       string               `json:"npc_curve"`
	Sources        []combatSource       `json:"sources"`
	Commits        []combatCommit       `json:"commits"`
}

func buildCombat(serverPath string) (combatCatalog, error) {
	// Resolve once so a new commit during generation cannot mix revisions.
	revision, err := combatGit(serverPath, "rev-parse", "HEAD")
	if err != nil {
		return combatCatalog{}, fmt.Errorf("revision: %w", err)
	}
	date, err := combatGit(serverPath, "show", "-s", "--format=%cs", revision)
	if err != nil {
		return combatCatalog{}, fmt.Errorf("revisionDate: %w", err)
	}
	report := combatCatalog{
		Revision: revision, ShortRevision: revision[:7], RevisionDate: date,
		RevisionURL: darkspinSourceURL + "/commit/" + revision,
	}
	files := make(map[string]*ast.File)
	for _, spec := range combatSourceSpecs {
		current, err := combatCommittedSource(serverPath, revision, spec.Path)
		if err != nil {
			return combatCatalog{}, fmt.Errorf("currentSource: %w", err)
		}
		reviewed, err := combatCommittedSource(serverPath, reviewedCombatRevision, spec.Path)
		if err != nil {
			return combatCatalog{}, fmt.Errorf("reviewedSource: %w", err)
		}
		for _, name := range spec.Functions {
			currentFunction, err := combatFunction(current, name)
			if err != nil {
				return combatCatalog{}, fmt.Errorf("currentFunction[%s]: %w", spec.Path, err)
			}
			reviewedFunction, err := combatFunction(reviewed, name)
			if err != nil {
				return combatCatalog{}, fmt.Errorf("reviewedFunction[%s]: %w", spec.Path, err)
			}
			if currentFunction != reviewedFunction {
				return combatCatalog{}, fmt.Errorf(
					"combat review required: %s:%s changed since %s; review the page and generator before updating reviewedCombatRevision",
					spec.Path, name, reviewedCombatRevision[:7],
				)
			}
		}
		files[spec.Path] = current
		report.Sources = append(report.Sources, combatSource{
			Name: spec.Name, URL: darkspinSourceURL + "/blob/" + revision + "/" + spec.Path,
		})
	}
	heroSource := files["server/combat/defense.go"]
	npcSource := files["server/zone/npc/defense.go"]
	for _, operand := range []struct {
		File        *ast.File
		Name        string
		Destination *float64
	}{
		{heroSource, "MaximumAvoidanceChance", &report.HeroAvoidance},
		{heroSource, "MaximumDamageReduction", &report.HeroMitigation},
		{npcSource, "maximumPassiveReduction", &report.NPCPassive},
		{npcSource, "maximumPhaseReduction", &report.NPCPhase},
	} {
		fraction, err := combatConstant(operand.File, operand.Name)
		if err != nil {
			return combatCatalog{}, fmt.Errorf("cap[%s]: %w", operand.Name, err)
		}
		if fraction <= 0 || fraction >= 1 || math.IsNaN(fraction) || math.IsInf(fraction, 0) {
			return combatCatalog{}, fmt.Errorf("cap[%s]: expected a fraction between zero and one", operand.Name)
		}
		*operand.Destination = fraction
	}
	if report.NPCPhase < report.NPCPassive {
		return combatCatalog{}, fmt.Errorf("phaseCap: below ordinary cap; review the report")
	}
	for _, ratio := range []float64{0, 0.25, 0.5, 1, 2, 5, 10} {
		report.RatingSamples = append(report.RatingSamples, combatRatingSample{
			RatingRatio:   ratio,
			HeroAvoidance: round3(100 * combatRatingFraction(ratio, report.HeroAvoidance)),
			NPCReduction:  round3(100 * combatRatingFraction(ratio, report.NPCPassive)),
		})
	}
	report.HeroCurve = combatCurve(report.HeroAvoidance)
	report.NPCCurve = combatCurve(report.NPCPassive)
	for _, sample := range []struct {
		Name    string
		Damage  float64
		General float64
		Science float64
		Armor   float64
	}{
		{"50% general + 50% science", 1000, 0.5, 0.5, 0},
		{"Same reductions + 100 flat armor", 1000, 0.5, 0.5, 100},
		{"Same reductions + 999 flat armor", 1000, 0.5, 0.5, 999},
		{"Small hit with two 90% reductions", 5, 0.9, 0.9, 0},
		{"Sub-one hit with two 90% reductions", 0.4, 0.9, 0.9, 0},
	} {
		remaining := sample.Damage*(1-min(sample.General, report.HeroMitigation))*
			(1-min(sample.Science, report.HeroMitigation)) - sample.Armor
		floor := min(sample.Damage, max(1, sample.Damage*(1-report.HeroMitigation)))
		report.HitSamples = append(report.HitSamples, combatHitSample{
			Name: sample.Name, Incoming: sample.Damage, BeforeCap: round3(remaining),
			Floor: round3(floor), Damage: round3(max(floor, remaining)),
		})
	}
	report.Commits, err = combatHistory(serverPath, revision)
	if err != nil {
		return combatCatalog{}, fmt.Errorf("history: %w", err)
	}
	return report, nil
}

func combatGit(serverPath string, arguments ...string) (string, error) {
	absolutePath, err := filepath.Abs(serverPath)
	if err != nil {
		return "", fmt.Errorf("sourcePath: %w", err)
	}
	// A command-local exception permits read-only inspection from the sandbox
	// account without changing the user's Git configuration.
	options := []string{"-c", "safe.directory=" + filepath.ToSlash(absolutePath), "-C", absolutePath}
	command := exec.Command("git", append(options, arguments...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("gitRead: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}

func combatCommittedSource(serverPath, revision, path string) (*ast.File, error) {
	source, err := combatGit(serverPath, "show", revision+":"+path)
	if err != nil {
		return nil, fmt.Errorf("sourceRead[%s]: %w", path, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		return nil, fmt.Errorf("sourceParse[%s]: %w", path, err)
	}
	return file, nil
}

func combatFunction(file *ast.File, name string) (string, error) {
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != name {
			continue
		}
		var source bytes.Buffer
		err := format.Node(&source, token.NewFileSet(), function)
		if err != nil {
			return "", fmt.Errorf("functionFormat[%s]: %w", name, err)
		}
		return source.String(), nil
	}
	return "", fmt.Errorf("functionMissing: %s", name)
}

func combatConstant(file *ast.File, name string) (float64, error) {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			constant, isConstant := spec.(*ast.ValueSpec)
			if !isConstant || len(constant.Names) != 1 || constant.Names[0].Name != name || len(constant.Values) != 1 {
				continue
			}
			expression := constant.Values[0]
			if cast, isCast := expression.(*ast.CallExpr); isCast && len(cast.Args) == 1 {
				expression = cast.Args[0]
			}
			literal, ok := expression.(*ast.BasicLit)
			if !ok {
				return 0, fmt.Errorf("constantParse[%s]: expected numeric literal", name)
			}
			fraction, err := strconv.ParseFloat(literal.Value, 64)
			if err != nil {
				return 0, fmt.Errorf("constantParse[%s]: %w", name, err)
			}
			return fraction, nil
		}
	}
	return 0, fmt.Errorf("constantMissing: %s", name)
}

func combatRatingFraction(ratio, ceiling float64) float64 {
	return ceiling * ratio / (ratio + ceiling)
}

func round3(amount float64) float64 {
	return math.Round(amount*1000) / 1000
}

func combatCurve(ceiling float64) string {
	points := make([]string, 0, 101)
	for index := 0; index <= 100; index++ {
		ratio := float64(index) / 20
		points = append(points, fmt.Sprintf("%.2f,%.2f", 50+100*ratio, 220-200*combatRatingFraction(ratio, ceiling)))
	}
	return strings.Join(points, " ")
}

func combatHistory(serverPath, revision string) ([]combatCommit, error) {
	history, err := combatGit(serverPath, "log", "-5", "--format=%H|%cs|%s", revision, "--",
		"server/combat/defense.go", "server/zone/npc/defense.go", "content/loot_affix.go")
	if err != nil {
		return nil, fmt.Errorf("commitLog: %w", err)
	}
	commits := make([]combatCommit, 0, 5)
	for _, line := range strings.Split(history, "\n") {
		fields := strings.SplitN(line, "|", 3)
		if len(fields) != 3 || len(fields[0]) < 7 {
			return nil, fmt.Errorf("commitFormat: invalid history record")
		}
		commits = append(commits, combatCommit{
			Revision: fields[0][:7], Date: fields[1], Summary: fields[2],
			URL: darkspinSourceURL + "/commit/" + fields[0],
		})
	}
	return commits, nil
}
