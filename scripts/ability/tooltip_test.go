package main

import (
	"testing"

	"github.com/darkspinnet/darkspin/content/lua51"
)

func tooltipABC(op lua51.Opcode, a, b, c uint32) lua51.Instruction {
	return lua51.Instruction(uint32(op) | a<<6 | b<<23 | c<<14)
}

func TestTooltipEvaluatesRankedArithmeticAndPreservesZero(t *testing.T) {
	// return math.floor(modifier.duration[rank] / 2)
	p := &lua51.Prototype{MaxStackSize: 5, Constants: []lua51.Constant{
		{Kind: lua51.ConstantString, String: "math"},
		{Kind: lua51.ConstantString, String: "floor"},
		{Kind: lua51.ConstantString, String: "modifier"},
		{Kind: lua51.ConstantString, String: "duration"},
		{Kind: lua51.ConstantNumber, Number: 2},
	}, Instructions: []lua51.Instruction{
		tooltipABC(lua51.OpcodeGetGlobal, 2, 0, 0),
		tooltipABC(lua51.OpcodeGetTable, 2, 2, 257),
		tooltipABC(lua51.OpcodeGetGlobal, 3, 0, 2),
		tooltipABC(lua51.OpcodeGetTable, 3, 3, 259),
		tooltipABC(lua51.OpcodeGetTable, 3, 3, 1),
		tooltipABC(lua51.OpcodeDiv, 3, 3, 260),
		tooltipABC(lua51.OpcodeCall, 2, 2, 2),
		tooltipABC(lua51.OpcodeReturn, 2, 2, 0),
	}}
	for _, test := range []struct{ duration, want float64 }{{8.25, 4}, {0, 0}} {
		globals := map[string]tooltipValue{"modifier": {kind: "table", table: map[string]tooltipValue{
			"duration": {kind: "table", array: []tooltipValue{tooltipNumber(test.duration)}},
		}}}
		got := evaluateTooltip(p, "duration", globals, tooltipValue{})
		if got.kind != "number" || got.number != test.want {
			t.Fatalf("duration %v: got %#v", test.duration, got)
		}
	}
}

func TestTooltipUsesWeaponPreviewAndRejectsUnknownCalls(t *testing.T) {
	p := &lua51.Prototype{MaxStackSize: 4, Constants: []lua51.Constant{
		{Kind: lua51.ConstantString, String: "nClient"},
		{Kind: lua51.ConstantString, String: "GetWeaponDamage"},
		{Kind: lua51.ConstantNumber, Number: 2},
	}, Instructions: []lua51.Instruction{
		tooltipABC(lua51.OpcodeGetGlobal, 2, 0, 0),
		tooltipABC(lua51.OpcodeGetTable, 2, 2, 257),
		tooltipABC(lua51.OpcodeCall, 2, 1, 2),
		tooltipABC(lua51.OpcodeGetTable, 2, 2, 258),
		tooltipABC(lua51.OpcodeReturn, 2, 2, 0),
	}}
	weapon := tooltipValue{kind: "table", array: []tooltipValue{tooltipNumber(10), tooltipNumber(16)}}
	if got := evaluateTooltip(p, "maxDamage", nil, weapon); got.kind != "number" || got.number != 16 {
		t.Fatalf("weapon preview = %#v", got)
	}
	p.Constants[1].String = "UnknownCallback"
	if got := evaluateTooltip(p, "maxDamage", nil, weapon); got.kind != "" {
		t.Fatalf("unknown host call = %#v", got)
	}
	p.Instructions = []lua51.Instruction{lua51.Instruction(uint32(lua51.OpcodeJmp) | 131070<<14)}
	if got := evaluateTooltip(p, "maxDamage", nil, weapon); got.kind != "" {
		t.Fatalf("unbounded loop = %#v", got)
	}
}

func TestDescriptionTokenMatchesSingleAndDoubleTildes(t *testing.T) {
	got := descriptionToken.FindAllStringSubmatch("~~minDamage~~–~maxDamage~; ~Modifier.duration~", -1)
	if len(got) != 3 || got[0][1] != "minDamage" || got[2][1] != "Modifier.duration" {
		t.Fatalf("tokens = %#v", got)
	}
}
