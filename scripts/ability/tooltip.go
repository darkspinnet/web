package main

import (
	"database/sql"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
)

var descriptionToken = regexp.MustCompile(`~+([A-Za-z][A-Za-z0-9_.]*)~+`)

type tooltipValue struct {
	kind    string
	number  float64
	text    string
	table   map[string]tooltipValue
	array   []tooltipValue
	closure *lua51.Prototype
}
type tooltipModule struct {
	globals     map[string]tooltipValue
	translators map[string]*lua51.Prototype
	aliases     map[string]string
}
type tooltipResolver struct {
	database *sql.DB
	heroes   []hero
	modules  map[int64]*tooltipModule
}

func tooltipNumber(n float64) tooltipValue { return tooltipValue{kind: "number", number: n} }
func tooltipStatic(value lua51.StaticValue) tooltipValue {
	switch value.Kind {
	case lua51.StaticNumber:
		return tooltipNumber(value.Number)
	case lua51.StaticString:
		return tooltipValue{kind: "string", text: value.String}
	case lua51.StaticTable:
		result := tooltipValue{kind: "table", table: map[string]tooltipValue{}}
		for k, v := range value.Fields {
			result.table[k] = tooltipStatic(v)
		}
		for _, v := range value.Entries {
			result.array = append(result.array, tooltipStatic(v))
		}
		return result
	}
	return tooltipValue{}
}
func tooltipConstant(p *lua51.Prototype, index uint32) tooltipValue {
	if int(index) >= len(p.Constants) {
		return tooltipValue{}
	}
	c := p.Constants[index]
	if c.Kind == lua51.ConstantNumber {
		return tooltipNumber(c.Number)
	}
	if c.Kind == lua51.ConstantString {
		return tooltipValue{kind: "string", text: c.String}
	}
	return tooltipValue{}
}
func tooltipKey(value, key tooltipValue) tooltipValue {
	if value.kind == "ref" && key.kind == "string" {
		return tooltipValue{kind: "function", text: value.text + "." + key.text}
	}
	if value.kind != "table" {
		return tooltipValue{}
	}
	if key.kind == "number" {
		index := int(key.number) - 1
		if key.number == float64(index+1) && index >= 0 && index < len(value.array) {
			return value.array[index]
		}
	}
	if key.kind == "string" {
		return value.table[key.text]
	}
	return tooltipValue{}
}
func tooltipRank(value tooltipValue) tooltipValue {
	if len(value.array) > 0 {
		return value.array[0]
	}
	return value
}

func (resolver *tooltipResolver) module(id int64) (*tooltipModule, error) {
	if module, ok := resolver.modules[id]; ok {
		return module, nil
	}
	root, err := loadDecodedChunk(resolver.database, id)
	if err != nil {
		return nil, err
	}
	chunk, err := lua51.Inspect(root.Bytecode)
	if err != nil {
		return nil, err
	}
	module := &tooltipModule{globals: map[string]tooltipValue{}, translators: map[string]*lua51.Prototype{}, aliases: map[string]string{}}
	for name, value := range lua51.StaticGlobals(chunk) {
		module.globals[name] = tooltipStatic(value)
	}
	p := chunk.Main
	register := make([]tooltipValue, int(p.MaxStackSize)+1)
	rk := func(index uint16) tooltipValue {
		if index >= 256 {
			return tooltipConstant(p, uint32(index-256))
		}
		if int(index) < len(register) {
			return register[index]
		}
		return tooltipValue{}
	}
	for _, instruction := range p.Instructions {
		a := int(instruction.A())
		if a >= len(register) {
			continue
		}
		switch instruction.Opcode() {
		case lua51.OpcodeMove:
			register[a] = register[instruction.B()]
		case lua51.OpcodeLoadK:
			register[a] = tooltipConstant(p, instruction.Bx())
		case lua51.OpcodeGetGlobal:
			v := tooltipConstant(p, instruction.Bx())
			register[a] = tooltipValue{kind: "ref", text: v.text}
		case lua51.OpcodeGetTable:
			register[a] = tooltipKey(register[instruction.B()], rk(instruction.C()))
		case lua51.OpcodeClosure:
			if int(instruction.Bx()) < len(p.Prototypes) {
				register[a] = tooltipValue{kind: "closure", closure: p.Prototypes[instruction.Bx()]}
			}
		case lua51.OpcodeSetTable:
			key, value := rk(instruction.B()), rk(instruction.C())
			if register[a].kind == "ref" && key.text == "TranslateToken" && value.closure != nil {
				module.translators[register[a].text] = value.closure
			}
		case lua51.OpcodeCall:
			fn := register[a].text
			if (strings.HasPrefix(fn, "nAbility.Register") || strings.HasPrefix(fn, "nModifier.Register")) && instruction.B() > 1 {
				name, namespace := "", ""
				for i := a + 1; i < a+int(instruction.B()) && i < len(register); i++ {
					if register[i].kind == "string" && name == "" {
						name = register[i].text
					}
					if register[i].kind == "ref" && (strings.HasPrefix(register[i].text, "nAbility_") || strings.HasPrefix(register[i].text, "nModifier_")) {
						namespace = register[i].text
					}
				}
				if name != "" && namespace != "" {
					module.aliases[strings.ToLower(name)] = namespace
				}
			}
			register[a] = tooltipValue{}
		}
	}
	resolver.modules[id] = module
	return module, nil
}

// Evaluate only bounded, side-effect-free TranslateToken arithmetic. Unknown
// opcodes or host calls stay unknown; no gameplay callbacks are executed.
func evaluateTooltip(p *lua51.Prototype, token string, globals map[string]tooltipValue, weapon tooltipValue) tooltipValue {
	if p == nil {
		return tooltipValue{}
	}
	register := make([]tooltipValue, int(p.MaxStackSize)+1)
	register[0] = tooltipValue{kind: "string", text: token}
	register[1] = tooltipNumber(1)
	rk := func(index uint16) tooltipValue {
		if index >= 256 {
			return tooltipConstant(p, uint32(index-256))
		}
		if int(index) < len(register) {
			return register[index]
		}
		return tooltipValue{}
	}
	call := func(fn tooltipValue, args []tooltipValue) tooltipValue {
		if fn.text == "nClient.GetWeaponDamage" {
			return weapon
		}
		if len(args) == 0 {
			return tooltipValue{}
		}
		switch fn.text {
		case "GetRankedValueHelper", "nAbility.GetRankedValue":
			return tooltipRank(args[0])
		case "math.floor":
			if args[0].kind == "number" {
				return tooltipNumber(math.Floor(args[0].number))
			}
		case "math.ceil":
			if args[0].kind == "number" {
				return tooltipNumber(math.Ceil(args[0].number))
			}
		}
		return tooltipValue{}
	}
	for pc, steps := 0, 0; pc < len(p.Instructions) && steps < 512; pc, steps = pc+1, steps+1 {
		if pc < 0 {
			return tooltipValue{}
		}
		instruction := p.Instructions[pc]
		a := int(instruction.A())
		if a >= len(register) {
			return tooltipValue{}
		}
		switch instruction.Opcode() {
		case lua51.OpcodeMove:
			register[a] = register[instruction.B()]
		case lua51.OpcodeLoadK:
			register[a] = tooltipConstant(p, instruction.Bx())
		case lua51.OpcodeGetGlobal:
			name := tooltipConstant(p, instruction.Bx()).text
			if value, ok := globals[name]; ok {
				register[a] = value
			} else {
				register[a] = tooltipValue{kind: "ref", text: name}
			}
		case lua51.OpcodeGetTable:
			register[a] = tooltipKey(register[instruction.B()], rk(instruction.C()))
		case lua51.OpcodeEq:
			left, right := rk(instruction.B()), rk(instruction.C())
			if left.kind == "" || right.kind == "" {
				return tooltipValue{}
			}
			equal := left.kind == right.kind && left.text == right.text && left.number == right.number
			if equal != (instruction.A() != 0) {
				pc++
			}
		case lua51.OpcodeJmp:
			pc += int(instruction.SBx())
		case lua51.OpcodeAdd, lua51.OpcodeSub, lua51.OpcodeMul, lua51.OpcodeDiv:
			left, right := rk(instruction.B()), rk(instruction.C())
			if left.kind != "number" || right.kind != "number" {
				register[a] = tooltipValue{}
				continue
			}
			n := left.number
			switch instruction.Opcode() {
			case lua51.OpcodeAdd:
				n += right.number
			case lua51.OpcodeSub:
				n -= right.number
			case lua51.OpcodeMul:
				n *= right.number
			case lua51.OpcodeDiv:
				if right.number == 0 {
					return tooltipValue{}
				}
				n /= right.number
			}
			register[a] = tooltipNumber(n)
		case lua51.OpcodeCall, lua51.OpcodeTailCall:
			if instruction.B() == 0 {
				return tooltipValue{}
			}
			end := a + int(instruction.B())
			if end > len(register) {
				return tooltipValue{}
			}
			value := call(register[a], register[a+1:end])
			if instruction.Opcode() == lua51.OpcodeTailCall {
				return value
			}
			register[a] = value
		case lua51.OpcodeReturn:
			if instruction.B() == 1 {
				return tooltipValue{}
			}
			return register[a]
		default:
			return tooltipValue{}
		}
	}
	return tooltipValue{}
}

func tooltipIdentity(name string) string {
	name = strings.ToLower(name)
	name = strings.TrimPrefix(name, "nability_")
	name = strings.TrimPrefix(name, "nmodifier_")
	name = strings.TrimSuffix(name, "modifier")
	return strings.ReplaceAll(name, "_", "")
}

func (resolver *tooltipResolver) resolve(entry *abilityEntry, compiled *compiledAbility, constants []luaConstant) error {
	if !descriptionToken.MatchString(entry.Description) {
		return nil
	}
	ids := map[int64]bool{}
	for _, constant := range constants {
		ids[constant.ChunkID] = true
	}
	type binding struct {
		token, source, property string
		index                   int
		multiplier              float64
	}
	bindings := []binding{}
	rows, err := resolver.database.Query(`SELECT DISTINCT b.lua_chunk_id,b.ability_table_name,b.token_name,b.source_table_name,b.property_name,b.element_index,b.multiplier FROM lua_token_binding b JOIN lua_string_constant c ON c.lua_chunk_id=b.lua_chunk_id WHERE c.string_constant=? COLLATE NOCASE`, entry.AssetName)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var namespace string
		var b binding
		if err := rows.Scan(&id, &namespace, &b.token, &b.source, &b.property, &b.index, &b.multiplier); err != nil {
			rows.Close()
			return err
		}
		if tooltipIdentity(namespace) == tooltipIdentity(entry.AssetName) {
			bindings = append(bindings, b)
			ids[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, b := range bindings {
		rows, err := resolver.database.Query(`SELECT DISTINCT lua_chunk_id FROM lua_static_property WHERE table_name=? AND property_name=?`, b.source, b.property)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids[id] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	for _, match := range descriptionToken.FindAllStringSubmatch(entry.Description, -1) {
		if parts := strings.SplitN(match[1], ".", 2); len(parts) == 2 {
			rows, err := resolver.database.Query("SELECT DISTINCT lua_chunk_id FROM lua_string_constant WHERE string_constant=? COLLATE NOCASE", parts[0])
			if err != nil {
				return err
			}
			for rows.Next() {
				var id int64
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids[id] = true
			}
			rows.Close()
		}
	}
	globals := map[string]tooltipValue{}
	translators := map[string]*lua51.Prototype{}
	aliases := map[string]string{}
	ordered := []int64{}
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, id := range ordered {
		modules, err := loadLuaModules(resolver.database, id)
		if err != nil {
			return err
		}
		list := []int64{id}
		for _, dependency := range modules {
			list = append(list, dependency.ChunkID)
		}
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		for _, moduleID := range list {
			module, err := resolver.module(moduleID)
			if err != nil {
				return err
			}
			for name, value := range module.globals {
				if value.kind == "table" {
					globals[name] = value
				}
			}
			for name, p := range module.translators {
				translators[name] = p
			}
			for name, namespace := range module.aliases {
				aliases[name] = namespace
			}
		}
	}
	namespace := aliases[strings.ToLower(entry.AssetName)]
	if compiled != nil && globals[compiled.Definition.Namespace].kind == "table" {
		namespace = compiled.Definition.Namespace
	}
	if namespace == "" {
		for name := range globals {
			if tooltipIdentity(name) == tooltipIdentity(entry.AssetName) {
				if namespace != "" && namespace != name {
					namespace = ""
					break
				}
				namespace = name
			}
		}
	}
	weapon := tooltipValue{}
	if len(entry.Heroes) == 1 {
		for _, profile := range resolver.heroes {
			if profile.Family == entry.Heroes[0].Name && profile.WeaponMinimum > 0 && profile.WeaponMaximum > 0 {
				candidate := tooltipValue{kind: "table", array: []tooltipValue{tooltipNumber(float64(profile.WeaponMinimum)), tooltipNumber(float64(profile.WeaponMaximum))}}
				if weapon.kind != "" && (weapon.array[0].number != candidate.array[0].number || weapon.array[1].number != candidate.array[1].number) {
					weapon = tooltipValue{}
					break
				}
				weapon = candidate
			}
		}
	}
	entry.DescriptionBasis = "Rank 1 base values; weapon damage uses the hero’s unmodified weapon profile. Equipment, attributes, and buffs can change live values."
	entry.Description = descriptionToken.ReplaceAllStringFunc(entry.Description, func(match string) string {
		token := descriptionToken.FindStringSubmatch(match)[1]
		if token == "CHAR_NAME" {
			return "the caster"
		}
		subject, property := namespace, token
		if parts := strings.SplitN(token, ".", 2); len(parts) == 2 {
			subject = aliases[strings.ToLower(parts[0])]
			property = parts[1]
			if subject == "" {
				for name := range globals {
					if tooltipIdentity(name) == tooltipIdentity(parts[0]) {
						subject = name
						break
					}
				}
			}
		}
		value := evaluateTooltip(translators[subject], property, globals, weapon)
		// Passive abilities often delegate their tooltip to an identically named
		// modifier. Only accept matching translators when they agree on a value.
		if value.kind != "number" {
			var candidate tooltipValue
			conflict := false
			for name, translator := range translators {
				if tooltipIdentity(name) != tooltipIdentity(subject) {
					continue
				}
				other := evaluateTooltip(translator, property, globals, weapon)
				if other.kind != "number" {
					continue
				}
				if candidate.kind == "number" && candidate.number != other.number {
					conflict = true
				}
				candidate = other
			}
			if !conflict {
				value = candidate
			}
		}
		if value.kind != "number" {
			for _, b := range bindings {
				if b.token != property {
					continue
				}
				bound := tooltipRank(globals[b.source].table[b.property])
				if b.index > 0 {
					bound = tooltipKey(bound, tooltipNumber(float64(b.index)))
				}
				if bound.kind == "number" {
					value = tooltipNumber(bound.number * b.multiplier)
					break
				}
			}
		}
		if value.kind != "number" {
			table := globals[subject]
			switch property {
			case "minDamage", "maxDamage":
				damage := tooltipRank(table.table["damage"])
				index := 1.0
				if property == "maxDamage" {
					index = 2
				}
				value = tooltipKey(damage, tooltipNumber(index))
			case "chainLength":
				value = tooltipRank(table.table["maxChains"])
			case "chainIncrease":
				v := table.table["reducedDamagePercentage"]
				if v.kind == "number" {
					value = tooltipNumber((1 - v.number) * 100)
				}
			default:
				value = tooltipRank(table.table[property])
			}
		}
		if value.kind == "number" && !math.IsNaN(value.number) && !math.IsInf(value.number, 0) {
			return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value.number, 'f', 3, 64), "0"), ".")
		}
		entry.UnresolvedTokens = appendUnique(entry.UnresolvedTokens, token)
		return "value unavailable"
	})
	return nil
}
