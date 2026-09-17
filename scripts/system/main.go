package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zoneresult "github.com/darkspinnet/darkspin/server/zone/result"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type probabilityRule struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Chance    float64 `json:"chance"`
	Outcome   string  `json:"outcome"`
	Condition string  `json:"condition"`
}

type equipmentRarity struct {
	Name            string  `json:"name"`
	Chance          float64 `json:"chance"`
	EnemyChance     float64 `json:"enemy_chance"`
	AffixCount      int     `json:"affix_count"`
	LevelAdjustment int32   `json:"level_adjustment"`
}

type capsuleSample struct {
	Name          string  `json:"name"`
	HealthPercent uint32  `json:"health_percent"`
	PowerPercent  uint32  `json:"power_percent"`
	HealthChance  float64 `json:"health_chance"`
	PowerChance   float64 `json:"power_chance"`
}

type dnaRange struct {
	Difficulty uint32 `json:"difficulty"`
	Minimum    uint32 `json:"minimum"`
	Typical    uint32 `json:"typical"`
	Maximum    uint32 `json:"maximum"`
}

type dropCatalog struct {
	RuleCount         int               `json:"rule_count"`
	EnemyRules        []probabilityRule `json:"enemy_rules"`
	EquipmentRarities []equipmentRarity `json:"equipment_rarities"`
	CapsuleSamples    []capsuleSample   `json:"capsule_samples"`
	DNARanges         []dnaRange        `json:"dna_ranges"`
	CapsuleBaseWeight float64           `json:"capsule_base_weight"`
	CapsuleNeedWeight float64           `json:"capsule_need_weight"`
	EquipmentSource   int64             `json:"equipment_source"`
	CapsuleSource     int64             `json:"capsule_source"`
	CatalystSource    int64             `json:"catalyst_source"`
}

type experienceLevel struct {
	Level           uint32 `json:"level"`
	MinimumXP       uint32 `json:"minimum_xp"`
	MaximumXP       uint32 `json:"maximum_xp"`
	XPToNext        uint32 `json:"xp_to_next"`
	IsOpenEnded     bool   `json:"is_open_ended"`
	IsHeroReward    bool   `json:"is_hero_reward"`
	HeroRewardTotal uint32 `json:"hero_reward_total"`
}

type experienceBand struct {
	Name   string            `json:"name"`
	Start  uint32            `json:"start"`
	End    uint32            `json:"end"`
	Levels []experienceLevel `json:"levels"`
}

type progressionCatalog struct {
	LevelCount            int              `json:"level_count"`
	MaximumLevelMinimumXP uint32           `json:"maximum_level_minimum_xp"`
	HeroRewardCount       int              `json:"hero_reward_count"`
	HeroRewardMilestones  []uint32         `json:"hero_reward_milestones"`
	Bands                 []experienceBand `json:"bands"`
}

type cashOutReward struct {
	PlanetsCompleted uint8  `json:"planets_completed"`
	Difficulty       uint32 `json:"difficulty"`
	PartCount        int    `json:"part_count"`
	ItemLevel        uint32 `json:"item_level"`
}

type cashOutRaritySample struct {
	Name             string `json:"name"`
	PlanetsCompleted uint8  `json:"planets_completed"`
	Bronze           uint32 `json:"bronze"`
	Silver           uint32 `json:"silver"`
	Gold             uint32 `json:"gold"`
	SpecialChance    uint32 `json:"special_chance"`
	RarifiedChance   uint32 `json:"rarified_chance"`
	PurifiedChance   uint32 `json:"purified_chance"`
}

type rewardCatalog struct {
	MaximumPartCount    int                   `json:"maximum_part_count"`
	MaximumCashOutLevel uint32                `json:"maximum_cash_out_level"`
	RewardLevelModel    string                `json:"reward_level_model"`
	CashOut             []cashOutReward       `json:"cash_out"`
	RaritySamples       []cashOutRaritySample `json:"rarity_samples"`
	HeroMilestones      []uint32              `json:"hero_milestones"`
}

type systemCatalog struct {
	ReportCount int                `json:"report_count"`
	Drops       dropCatalog        `json:"drops"`
	Progression progressionCatalog `json:"progression"`
	Rewards     rewardCatalog      `json:"rewards"`
}

func main() {
	serverPath := flag.String("server", filepath.Join("..", "darkspin"), "Darkspin source repository")
	outputPath := flag.String("out", filepath.Join("data", "generated", "system.json"), "generated system catalog")
	flag.Parse()
	result, err := build(*serverPath)
	if err != nil {
		fatal(err)
	}
	err = catalog.WriteJSON(*outputPath, result)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("system: wrote %d runtime reports to %s\n", result.ReportCount, *outputPath)
}

func build(serverPath string) (systemCatalog, error) {
	gameplayPath := filepath.Join(serverPath, "server", "gameplay", "interaction.go")
	lootPath := filepath.Join(serverPath, "server", "zone", "loot", "loot.go")
	orbPath := filepath.Join(serverPath, "server", "sim", "orb.go")
	partCatalogPath := filepath.Join(serverPath, "server", "game", "part_catalog.go")
	progressionPath := filepath.Join(serverPath, "server", "sporenet", "user_features.go")
	rewardPath := filepath.Join(serverPath, "server", "zone", "result", "reward.go")

	gameplayFile, err := parseSource(gameplayPath)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("gameplayParse: %w", err)
	}
	lootFile, err := parseSource(lootPath)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("lootParse: %w", err)
	}
	orbFile, err := parseSource(orbPath)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("orbParse: %w", err)
	}
	partCatalogFile, err := parseSource(partCatalogPath)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("partCatalogParse: %w", err)
	}
	progressionFile, err := parseSource(progressionPath)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("progressionParse: %w", err)
	}
	rewardFile, err := parseSource(rewardPath)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("rewardParse: %w", err)
	}

	equipmentSource, err := constantInteger(gameplayFile, "campaignNPCEquipmentSourceAmount")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("equipmentSource: %w", err)
	}
	capsuleSource, err := constantInteger(gameplayFile, "campaignNPCOrbSourceAmount")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("capsuleSource: %w", err)
	}
	catalystSource, err := constantInteger(gameplayFile, "campaignNPCCrystalSourceAmount")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("catalystSource: %w", err)
	}
	dnaThreshold, err := constantInteger(lootFile, "dnaChanceThreshold")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("dnaThreshold: %w", err)
	}
	dnaBasis, err := constantInteger(lootFile, "DNAChanceBasis")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("dnaBasis: %w", err)
	}
	baseWeight, needWeight, err := orbWeightCoefficients(orbFile)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("orbWeights: %w", err)
	}
	rarityThresholds, err := functionLessThanThresholds(partCatalogFile, "campaignPartRarity")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("equipmentRarity: %w", err)
	}

	drops, err := buildDrops(
		equipmentSource, capsuleSource, catalystSource,
		dnaThreshold, dnaBasis, baseWeight, needWeight, rarityThresholds,
	)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("drops: %w", err)
	}
	bounds, err := variableIntegerArray(progressionFile, "accountExperienceUpperBound")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("experienceBounds: %w", err)
	}
	milestones, err := functionIntegerArray(progressionFile, "heroRewardEntitlementCount", "milestones")
	if err != nil {
		return systemCatalog{}, fmt.Errorf("heroMilestones: %w", err)
	}
	progression, err := buildProgression(bounds, milestones)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("progression: %w", err)
	}
	cashOutRewards, rewardLevelModel, err := sourceCashOutRewards(rewardFile)
	if err != nil {
		return systemCatalog{}, fmt.Errorf("cashOutLevels: %w", err)
	}
	rewards := buildRewards(milestones, cashOutRewards, rewardLevelModel)
	return systemCatalog{ReportCount: 5, Drops: drops, Progression: progression, Rewards: rewards}, nil
}

func buildDrops(
	equipmentSource int64, capsuleSource int64, catalystSource int64,
	dnaThreshold int64, dnaBasis int64, baseWeight float64, needWeight float64,
	rarityThresholds []int64,
) (dropCatalog, error) {
	equipmentChance, err := equipmentProbability(int32(equipmentSource))
	if err != nil {
		return dropCatalog{}, err
	}
	capsuleBudget, err := sim.PlanOrbDropBudget(int32(capsuleSource), 1)
	if err != nil {
		return dropCatalog{}, fmt.Errorf("capsuleBudget: %w", err)
	}
	if capsuleBudget.GuaranteedSelection != 0 {
		return dropCatalog{}, errors.New("ordinary capsule budget is no longer probabilistic")
	}
	catalystSuccess := 0
	for draw := uint32(0); draw < 100; draw++ {
		isDrop, dropErr := zoneloot.IsCrystalDrop(int32(catalystSource), draw)
		if dropErr != nil {
			return dropCatalog{}, fmt.Errorf("catalystDraw[%d]: %w", draw, dropErr)
		}
		if isDrop {
			catalystSuccess++
		}
	}
	if dnaBasis <= 0 || dnaThreshold < 0 || dnaThreshold > dnaBasis {
		return dropCatalog{}, errors.New("DNA chance operands invalid")
	}
	dnaSuccess := 0
	for draw := uint32(0); draw < uint32(dnaBasis); draw++ {
		isDrop, dropErr := zoneloot.IsNPCDNADrop(draw)
		if dropErr != nil {
			return dropCatalog{}, fmt.Errorf("dnaDraw[%d]: %w", draw, dropErr)
		}
		if isDrop {
			dnaSuccess++
		}
	}
	if dnaSuccess != int(dnaThreshold) {
		return dropCatalog{}, fmt.Errorf("DNA threshold mismatch: got %d, want %d", dnaSuccess, dnaThreshold)
	}
	equipmentRarities, err := buildEquipmentRarities(equipmentChance, rarityThresholds)
	if err != nil {
		return dropCatalog{}, fmt.Errorf("equipmentRarities: %w", err)
	}
	rules := []probabilityRule{
		{Key: "equipment", Name: "Equipment", Chance: equipmentChance, Outcome: "One compatible rarity-rolled part", Condition: "Ordinary campaign enemies; disabled in the tutorial"},
		{Key: "capsule", Name: "Health / power capsule", Chance: float64(capsuleBudget.RemainderThreshold), Outcome: "One capsule weighted by squad need", Condition: "Ordinary enemies; tutorial enemies only after capsule drops unlock"},
		{Key: "catalyst", Name: "Catalyst", Chance: float64(catalystSuccess), Outcome: "One eligible catalyst", Condition: "Only after catalyst drops are unlocked"},
		{Key: "dna", Name: "DNA", Chance: round1(100 * float64(dnaSuccess) / float64(dnaBasis)), Outcome: "A difficulty-scaled DNA pickup", Condition: "Ordinary campaign enemies; disabled in the tutorial"},
	}
	sampleInputs := []struct {
		name          string
		health, power uint32
	}{
		{name: "Full squad resources", health: 100, power: 100},
		{name: "Balanced pressure", health: 50, power: 50},
		{name: "Health pressure", health: 25, power: 100},
		{name: "Power pressure", health: 100, power: 25},
		{name: "Critical health", health: 0, power: 100},
	}
	samples := make([]capsuleSample, 0, len(sampleInputs))
	for _, input := range sampleInputs {
		healthWeight := baseWeight + needWeight*(1-float64(input.health)/100)
		powerWeight := baseWeight + needWeight*(1-float64(input.power)/100)
		healthChance := 100 * healthWeight / (healthWeight + powerWeight)
		samples = append(samples, capsuleSample{
			Name: input.name, HealthPercent: input.health, PowerPercent: input.power,
			HealthChance: round1(healthChance), PowerChance: round1(100 - healthChance),
		})
	}
	difficulties := []uint32{1, 10, 25, 50, 75, 100}
	dnaRanges := make([]dnaRange, 0, len(difficulties))
	for _, difficulty := range difficulties {
		minimum, amountErr := zoneloot.NPCDNAAmount(difficulty, 0)
		if amountErr != nil {
			return dropCatalog{}, fmt.Errorf("dnaMinimum[%d]: %w", difficulty, amountErr)
		}
		typical, amountErr := zoneloot.NPCDNAAmount(difficulty, uint32(dnaBasis/2))
		if amountErr != nil {
			return dropCatalog{}, fmt.Errorf("dnaTypical[%d]: %w", difficulty, amountErr)
		}
		maximum, amountErr := zoneloot.NPCDNAAmount(difficulty, uint32(dnaBasis))
		if amountErr != nil {
			return dropCatalog{}, fmt.Errorf("dnaMaximum[%d]: %w", difficulty, amountErr)
		}
		dnaRanges = append(dnaRanges, dnaRange{Difficulty: difficulty, Minimum: minimum, Typical: typical, Maximum: maximum})
	}
	return dropCatalog{
		RuleCount: len(rules), EnemyRules: rules, EquipmentRarities: equipmentRarities,
		CapsuleSamples: samples, DNARanges: dnaRanges,
		CapsuleBaseWeight: baseWeight, CapsuleNeedWeight: needWeight,
		EquipmentSource: equipmentSource, CapsuleSource: capsuleSource, CatalystSource: catalystSource,
	}, nil
}

func buildEquipmentRarities(equipmentChance float64, thresholds []int64) ([]equipmentRarity, error) {
	if len(thresholds) != 3 || thresholds[0] <= 0 || thresholds[0] >= thresholds[1] ||
		thresholds[1] >= thresholds[2] || thresholds[2] >= 100 {
		return nil, fmt.Errorf("thresholds invalid: %v", thresholds)
	}
	names := []string{"Basic", "Uncommon", "Rare", "Epic"}
	affixCounts := []int{0, 1, 2, 3}
	levelAdjustments := []int32{-5, 0, 5, 10}
	boundaries := []int64{0, thresholds[0], thresholds[1], thresholds[2], 100}
	rarities := make([]equipmentRarity, 0, len(names))
	for index, name := range names {
		chance := float64(boundaries[index+1] - boundaries[index])
		rarities = append(rarities, equipmentRarity{
			Name: name, Chance: chance, EnemyChance: round3(equipmentChance * chance / 100),
			AffixCount: affixCounts[index], LevelAdjustment: levelAdjustments[index],
		})
	}
	return rarities, nil
}

func equipmentProbability(challenge int32) (float64, error) {
	lower := float64(0)
	upper := float64(1)
	for range 64 {
		middle := (lower + upper) / 2
		isDrop, err := zoneloot.IsEquipmentDrop(challenge, middle)
		if err != nil {
			return 0, fmt.Errorf("equipmentDraw: %w", err)
		}
		if isDrop {
			lower = middle
			continue
		}
		upper = middle
	}
	return round1(upper * 100), nil
}

func buildProgression(bounds []int64, milestones []int64) (progressionCatalog, error) {
	if len(bounds) == 0 || bounds[len(bounds)-1] >= math.MaxUint32 {
		return progressionCatalog{}, errors.New("experience bounds invalid")
	}
	milestoneSet := make(map[uint32]bool, len(milestones))
	milestoneLevels := make([]uint32, 0, len(milestones))
	for _, milestone := range milestones {
		if milestone < 1 || milestone > int64(len(bounds)+1) {
			return progressionCatalog{}, fmt.Errorf("hero milestone out of range: %d", milestone)
		}
		level := uint32(milestone)
		milestoneSet[level] = true
		milestoneLevels = append(milestoneLevels, level)
	}
	levels := make([]experienceLevel, 0, len(bounds)+1)
	rewardTotal := uint32(0)
	for index := 0; index <= len(bounds); index++ {
		level := uint32(index + 1)
		minimum := uint32(0)
		if index > 0 {
			minimum = uint32(bounds[index-1] + 1)
		}
		if sporenet.AccountLevelForExperience(minimum) != level {
			return progressionCatalog{}, fmt.Errorf("levelMinimum[%d]: %d", level, minimum)
		}
		entry := experienceLevel{Level: level, MinimumXP: minimum, IsOpenEnded: index == len(bounds)}
		if index < len(bounds) {
			entry.MaximumXP = uint32(bounds[index])
			entry.XPToNext = entry.MaximumXP + 1 - entry.MinimumXP
			if sporenet.AccountLevelForExperience(entry.MaximumXP) != level {
				return progressionCatalog{}, fmt.Errorf("levelMaximum[%d]: %d", level, entry.MaximumXP)
			}
		}
		entry.IsHeroReward = milestoneSet[level]
		if entry.IsHeroReward {
			rewardTotal++
		}
		entry.HeroRewardTotal = rewardTotal
		levels = append(levels, entry)
	}
	bands := make([]experienceBand, 0, (len(levels)+9)/10)
	for start := 0; start < len(levels); start += 10 {
		end := min(start+10, len(levels))
		bandLevels := append([]experienceLevel(nil), levels[start:end]...)
		bands = append(bands, experienceBand{
			Name:  fmt.Sprintf("Levels %d–%d", bandLevels[0].Level, bandLevels[len(bandLevels)-1].Level),
			Start: bandLevels[0].Level, End: bandLevels[len(bandLevels)-1].Level, Levels: bandLevels,
		})
	}
	return progressionCatalog{
		LevelCount: len(levels), MaximumLevelMinimumXP: levels[len(levels)-1].MinimumXP,
		HeroRewardCount: len(milestoneLevels), HeroRewardMilestones: milestoneLevels, Bands: bands,
	}, nil
}

func buildRewards(milestones []int64, rewards []cashOutReward, rewardLevelModel string) rewardCatalog {
	rarityInputs := []struct {
		name   string
		medals zoneresult.MedalCount
	}{
		{name: "No medals"},
		{name: "Bronze sweep", medals: zoneresult.MedalCount{Bronze: 4}},
		{name: "Silver sweep", medals: zoneresult.MedalCount{Silver: 4}},
		{name: "Gold sweep", medals: zoneresult.MedalCount{Gold: 4}},
		{name: "All medal grades", medals: zoneresult.MedalCount{Bronze: 4, Silver: 4, Gold: 4}},
	}
	raritySamples := make([]cashOutRaritySample, 0, len(rarityInputs))
	for _, input := range rarityInputs {
		raritySamples = append(raritySamples, cashOutSample(input.name, 4, input.medals))
	}
	heroMilestones := make([]uint32, 0, len(milestones))
	for _, milestone := range milestones {
		heroMilestones = append(heroMilestones, uint32(milestone))
	}
	return rewardCatalog{
		MaximumPartCount: zoneresult.RewardCount(4), MaximumCashOutLevel: rewards[len(rewards)-1].ItemLevel,
		RewardLevelModel: rewardLevelModel, CashOut: rewards,
		RaritySamples: raritySamples, HeroMilestones: heroMilestones,
	}
}

func sourceCashOutRewards(file *ast.File) ([]cashOutReward, string, error) {
	parameterCount, err := functionParameterCount(file, "RewardLevel")
	if err != nil {
		return nil, "", err
	}
	model := "completion_depth"
	if parameterCount == 2 {
		model = "difficulty"
	}
	rewards := make([]cashOutReward, 0, 4)
	for planets := uint8(1); planets <= 4; planets++ {
		difficulty := uint32(planets)
		level := uint32(0)
		switch parameterCount {
		case 1:
			firstLevel, constantErr := constantInteger(file, "firstRewardLevel")
			if constantErr != nil {
				return nil, "", fmt.Errorf("firstLevel: %w", constantErr)
			}
			level = uint32(firstLevel + 2*(int64(planets)-1))
		case 2:
			planetsPerChain, constantErr := constantInteger(file, "campaignPlanetsPerChain")
			if constantErr != nil {
				return nil, "", fmt.Errorf("planetsPerChain: %w", constantErr)
			}
			majorScale, constantErr := constantInteger(file, "campaignMajorLevelScale")
			if constantErr != nil {
				return nil, "", fmt.Errorf("majorScale: %w", constantErr)
			}
			boundaryBonus, constantErr := constantInteger(file, "campaignBoundaryBonus")
			if constantErr != nil {
				return nil, "", fmt.Errorf("boundaryBonus: %w", constantErr)
			}
			major := (int64(difficulty)-1)/planetsPerChain + 1
			minor := (int64(difficulty)-1)%planetsPerChain + 1
			levelAmount := majorScale*major + minor
			if minor == planetsPerChain {
				levelAmount += boundaryBonus
			} else {
				levelAmount += min(int64(planets)-1, planetsPerChain-1)
			}
			level = uint32(levelAmount)
		default:
			return nil, "", fmt.Errorf("RewardLevel parameter count %d unsupported", parameterCount)
		}
		rewards = append(rewards, cashOutReward{
			PlanetsCompleted: planets, Difficulty: difficulty,
			PartCount: zoneresult.RewardCount(planets), ItemLevel: level,
		})
	}
	return rewards, model, nil
}

func cashOutSample(name string, planetsCompleted uint8, medals zoneresult.MedalCount) cashOutRaritySample {
	bands := zoneresult.CashOutRarityBands(planetsCompleted, medals)
	sample := cashOutRaritySample{
		Name: name, PlanetsCompleted: planetsCompleted,
		Bronze: medals.Bronze, Silver: medals.Silver, Gold: medals.Gold,
	}
	for roll := uint32(1); roll <= 100; roll++ {
		switch zoneresult.CashOutRewardTier(roll, bands) {
		case zoneresult.RewardTierSpecial:
			sample.SpecialChance++
		case zoneresult.RewardTierRarified:
			sample.RarifiedChance++
		case zoneresult.RewardTierPurified:
			sample.PurifiedChance++
		}
	}
	return sample
}

func parseSource(path string) (*ast.File, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("sourceOpen[%s]: %w", path, err)
	}
	return file, nil
}

func constantInteger(file *ast.File, name string) (int64, error) {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			valueSpec, isValue := specification.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for index, identifier := range valueSpec.Names {
				if identifier.Name != name || index >= len(valueSpec.Values) {
					continue
				}
				return integerExpression(valueSpec.Values[index])
			}
		}
	}
	return 0, fmt.Errorf("constant %s not found", name)
}

func variableIntegerArray(file *ast.File, name string) ([]int64, error) {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			valueSpec, isValue := specification.(*ast.ValueSpec)
			if !isValue || len(valueSpec.Names) != 1 || valueSpec.Names[0].Name != name || len(valueSpec.Values) != 1 {
				continue
			}
			return compositeIntegers(valueSpec.Values[0])
		}
	}
	return nil, fmt.Errorf("variable array %s not found", name)
}

func functionIntegerArray(file *ast.File, functionName string, variableName string) ([]int64, error) {
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != functionName {
			continue
		}
		var result []int64
		var inspectErr error
		ast.Inspect(function.Body, func(node ast.Node) bool {
			assignment, isAssignment := node.(*ast.AssignStmt)
			if !isAssignment || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
				return true
			}
			identifier, isIdentifier := assignment.Lhs[0].(*ast.Ident)
			if !isIdentifier || identifier.Name != variableName {
				return true
			}
			result, inspectErr = compositeIntegers(assignment.Rhs[0])
			return false
		})
		if inspectErr != nil {
			return nil, inspectErr
		}
		if result != nil {
			return result, nil
		}
	}
	return nil, fmt.Errorf("function array %s.%s not found", functionName, variableName)
}

func functionLessThanThresholds(file *ast.File, functionName string) ([]int64, error) {
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != functionName {
			continue
		}
		thresholds := make([]int64, 0, 3)
		var inspectErr error
		ast.Inspect(function.Body, func(node ast.Node) bool {
			clause, isClause := node.(*ast.CaseClause)
			if !isClause || len(clause.List) != 1 || inspectErr != nil {
				return true
			}
			comparison, isComparison := clause.List[0].(*ast.BinaryExpr)
			if !isComparison || comparison.Op != token.LSS {
				return true
			}
			threshold, err := integerExpression(comparison.Y)
			if err != nil {
				inspectErr = err
				return false
			}
			thresholds = append(thresholds, threshold)
			return true
		})
		if inspectErr != nil {
			return nil, inspectErr
		}
		if len(thresholds) == 0 {
			return nil, fmt.Errorf("function %s has no thresholds", functionName)
		}
		return thresholds, nil
	}
	return nil, fmt.Errorf("function %s not found", functionName)
}

func functionParameterCount(file *ast.File, functionName string) (int, error) {
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != functionName {
			continue
		}
		count := 0
		for _, field := range function.Type.Params.List {
			count += max(1, len(field.Names))
		}
		return count, nil
	}
	return 0, fmt.Errorf("function %s not found", functionName)
}

func compositeIntegers(expression ast.Expr) ([]int64, error) {
	composite, isComposite := expression.(*ast.CompositeLit)
	if !isComposite {
		return nil, errors.New("expression is not a composite literal")
	}
	integers := make([]int64, 0, len(composite.Elts))
	for index, element := range composite.Elts {
		integer, err := integerExpression(element)
		if err != nil {
			return nil, fmt.Errorf("element[%d]: %w", index, err)
		}
		integers = append(integers, integer)
	}
	return integers, nil
}

func integerExpression(expression ast.Expr) (int64, error) {
	switch current := expression.(type) {
	case *ast.BasicLit:
		integer, err := strconv.ParseInt(current.Value, 0, 64)
		if err != nil {
			return 0, fmt.Errorf("integerParse[%s]: %w", current.Value, err)
		}
		return integer, nil
	case *ast.CallExpr:
		if len(current.Args) != 1 {
			return 0, errors.New("typed integer has multiple arguments")
		}
		return integerExpression(current.Args[0])
	case *ast.ParenExpr:
		return integerExpression(current.X)
	case *ast.UnaryExpr:
		integer, err := integerExpression(current.X)
		if err != nil {
			return 0, err
		}
		if current.Op == token.SUB {
			return -integer, nil
		}
		if current.Op == token.ADD {
			return integer, nil
		}
	}
	return 0, fmt.Errorf("unsupported integer expression %T", expression)
}

func orbWeightCoefficients(file *ast.File) (float64, float64, error) {
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != "orbResourceWeights" {
			continue
		}
		for index := len(function.Body.List) - 1; index >= 0; index-- {
			returnStatement, isReturn := function.Body.List[index].(*ast.ReturnStmt)
			if !isReturn || len(returnStatement.Results) != 3 {
				continue
			}
			base, need, coefficientErr := weightExpression(returnStatement.Results[0])
			if coefficientErr != nil {
				return 0, 0, coefficientErr
			}
			otherBase, otherNeed, coefficientErr := weightExpression(returnStatement.Results[1])
			if coefficientErr != nil {
				return 0, 0, coefficientErr
			}
			if base != otherBase || need != otherNeed {
				return 0, 0, errors.New("health and power weights diverged")
			}
			return base, need, nil
		}
	}
	return 0, 0, errors.New("orb resource weight return not found")
}

func weightExpression(expression ast.Expr) (float64, float64, error) {
	addition, isAddition := expression.(*ast.BinaryExpr)
	if !isAddition || addition.Op != token.ADD {
		return 0, 0, errors.New("orb weight is not an addition")
	}
	base, err := floatExpression(addition.X)
	if err != nil {
		return 0, 0, fmt.Errorf("base: %w", err)
	}
	multiplication, isMultiplication := addition.Y.(*ast.BinaryExpr)
	if !isMultiplication || multiplication.Op != token.MUL {
		return 0, 0, errors.New("orb need weight is not a multiplication")
	}
	need, err := floatExpression(multiplication.X)
	if err != nil {
		return 0, 0, fmt.Errorf("need: %w", err)
	}
	return base, need, nil
}

func floatExpression(expression ast.Expr) (float64, error) {
	switch current := expression.(type) {
	case *ast.BasicLit:
		amount, err := strconv.ParseFloat(current.Value, 64)
		if err != nil {
			return 0, fmt.Errorf("floatParse[%s]: %w", current.Value, err)
		}
		return amount, nil
	case *ast.ParenExpr:
		return floatExpression(current.X)
	}
	return 0, fmt.Errorf("unsupported float expression %T", expression)
}

func round1(amount float64) float64 {
	return math.Round(amount*10) / 10
}

func round3(amount float64) float64 {
	return math.Round(amount*1000) / 1000
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
