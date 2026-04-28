//go:build cgo

package ts

import (
	"strings"
	"testing"

	tsjs "github.com/smacker/go-tree-sitter/javascript"
	tsts "github.com/smacker/go-tree-sitter/typescript/typescript"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	"github.com/devchan97/code-map/internal/core"
)

// findSymbol returns the first symbol with the given qualname suffix, or nil.
func findSymbol(syms []core.Symbol, qualnameSuffix string) *core.Symbol {
	for i := range syms {
		if strings.HasSuffix(syms[i].Qualname, qualnameSuffix) {
			return &syms[i]
		}
	}
	return nil
}

// findEdge returns the first edge matching from/to/kind, or nil.
func findEdge(edges []core.Edge, from, to string, kind core.EdgeKind) *core.Edge {
	for i := range edges {
		if edges[i].Kind == kind &&
			strings.Contains(edges[i].FromQualname, from) &&
			strings.Contains(edges[i].ToQualname, to) {
			return &edges[i]
		}
	}
	return nil
}

// --- JavaScript tests ---

const jsFixture = `
import fs from 'fs';
import { readFile, writeFile } from 'path';

const MAX_RETRIES = 3;
let counter = 0;

function processData(input, options) {
    const result = input.trim();
    console.log(result);
    return result;
}

class EventEmitter {
    constructor() {
        this.listeners = [];
    }

    on(event, handler) {
        this.listeners.push(handler);
    }
}

class MyEmitter extends EventEmitter {
    emit(event) {
        console.log(event);
    }
}
`

func TestParse_JS_FunctionAndClass(t *testing.T) {
	a := &adapter{lang: "js", getLang: tsjs.GetLanguage}
	syms, edges, err := a.Parse("src/emitter.js", []byte(jsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Top-level function.
	fn := findSymbol(syms, ".processData")
	if fn == nil || fn.Kind != core.SymbolFunction || fn.Scope != core.ScopeGlobal {
		t.Errorf("processData function not detected: %v", fn)
	}

	// Class.
	emitter := findSymbol(syms, ".EventEmitter")
	if emitter == nil || emitter.Kind != core.SymbolClass {
		t.Errorf("EventEmitter class not detected: %v", emitter)
	}

	// Method.
	on := findSymbol(syms, ".EventEmitter.on")
	if on == nil || on.Kind != core.SymbolMethod || on.Scope != core.ScopeClass {
		t.Errorf("EventEmitter.on method not detected: %v", on)
	}

	// Inheritance: MyEmitter extends EventEmitter.
	myEmitter := findSymbol(syms, ".MyEmitter")
	if myEmitter == nil {
		t.Fatalf("MyEmitter not detected")
	}

	inheritEdge := findEdge(edges, "MyEmitter", "EventEmitter", core.EdgeInherit)
	if inheritEdge == nil {
		t.Errorf("MyEmitter→EventEmitter inherit edge not detected")
	}

	// Constant.
	maxRetries := findSymbol(syms, ".MAX_RETRIES")
	if maxRetries == nil || maxRetries.Kind != core.SymbolConstant {
		t.Errorf("MAX_RETRIES constant not detected: %v", maxRetries)
	}
}

func TestParse_JS_Imports(t *testing.T) {
	a := &adapter{lang: "js", getLang: tsjs.GetLanguage}
	syms, edges, err := a.Parse("src/emitter.js", []byte(jsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	importCount := 0
	for _, s := range syms {
		if s.Kind == core.SymbolImport {
			importCount++
		}
	}
	if importCount < 2 {
		t.Errorf("expected ≥2 import symbols; got %d", importCount)
	}

	importEdge := findEdge(edges, "", "fs", core.EdgeImport)
	if importEdge == nil {
		t.Errorf("import edge for 'fs' not detected")
	}
}

func TestParse_JS_ParamAndLocal(t *testing.T) {
	a := &adapter{lang: "js", getLang: tsjs.GetLanguage}
	syms, _, err := a.Parse("src/emitter.js", []byte(jsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasParam := false
	hasLocal := false
	for _, s := range syms {
		if s.Scope == core.ScopeParam && (s.Name == "input" || s.Name == "options") {
			hasParam = true
		}
		if s.Scope == core.ScopeLocal && s.Name == "result" {
			hasLocal = true
		}
	}
	if !hasParam {
		t.Errorf("expected param symbols input/options")
	}
	if !hasLocal {
		t.Errorf("expected local symbol 'result'")
	}
}

func TestParse_JS_CallEdge(t *testing.T) {
	a := &adapter{lang: "js", getLang: tsjs.GetLanguage}
	_, edges, err := a.Parse("src/emitter.js", []byte(jsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	callEdge := findEdge(edges, "processData", "console.log", core.EdgeCall)
	if callEdge == nil {
		t.Errorf("call edge for console.log not detected")
	}
}

// --- TypeScript tests ---

const tsFixture = `
import { EventEmitter } from 'events';

interface Describable {
    describe(): string;
}

const API_URL: string = "https://example.com";

function fetchData(url: string, timeout: number): Promise<string> {
    const fullUrl = url + "/api";
    return Promise.resolve(fullUrl);
}

class Service extends EventEmitter implements Describable {
    private name: string;

    constructor(name: string) {
        super();
        this.name = name;
    }

    describe(): string {
        return this.name;
    }
}
`

func TestParse_TS_ClassAndMethod(t *testing.T) {
	a := &adapter{lang: "ts", getLang: tsts.GetLanguage}
	syms, edges, err := a.Parse("src/service.ts", []byte(tsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Function.
	fn := findSymbol(syms, ".fetchData")
	if fn == nil || fn.Kind != core.SymbolFunction {
		t.Errorf("fetchData function not detected: %v", fn)
	}

	// Class.
	service := findSymbol(syms, ".Service")
	if service == nil || service.Kind != core.SymbolClass {
		t.Errorf("Service class not detected: %v", service)
	}

	// Method.
	describe := findSymbol(syms, ".Service.describe")
	if describe == nil || describe.Kind != core.SymbolMethod {
		t.Errorf("Service.describe method not detected: %v", describe)
	}

	// Inheritance: Service extends EventEmitter.
	inheritEdge := findEdge(edges, "Service", "EventEmitter", core.EdgeInherit)
	if inheritEdge == nil {
		t.Errorf("Service→EventEmitter inherit edge not detected")
	}
}

func TestParse_TS_ParamAndLocal(t *testing.T) {
	a := &adapter{lang: "ts", getLang: tsts.GetLanguage}
	syms, _, err := a.Parse("src/service.ts", []byte(tsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasParam := false
	hasLocal := false
	for _, s := range syms {
		if s.Scope == core.ScopeParam && (s.Name == "url" || s.Name == "timeout") {
			hasParam = true
		}
		if s.Scope == core.ScopeLocal && s.Name == "fullUrl" {
			hasLocal = true
		}
	}
	if !hasParam {
		t.Errorf("expected param symbols url/timeout")
	}
	if !hasLocal {
		t.Errorf("expected local symbol 'fullUrl'")
	}
}

// --- TSX tests ---

const tsxFixture = `
import React from 'react';
import { useState } from 'react';

const TITLE = "My App";

interface Props {
    name: string;
}

function Greeting(props) {
    const message = "Hello, " + props.name;
    return <div>{message}</div>;
}

class Counter extends React.Component {
    constructor(props) {
        super(props);
    }

    render() {
        return <span>0</span>;
    }
}
`

func TestParse_TSX_ComponentAndClass(t *testing.T) {
	a := &adapter{lang: "tsx", getLang: tstsx.GetLanguage}
	syms, edges, err := a.Parse("src/App.tsx", []byte(tsxFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Function component.
	greeting := findSymbol(syms, ".Greeting")
	if greeting == nil || greeting.Kind != core.SymbolFunction {
		t.Errorf("Greeting function component not detected: %v", greeting)
	}

	// Class component.
	counter := findSymbol(syms, ".Counter")
	if counter == nil || counter.Kind != core.SymbolClass {
		t.Errorf("Counter class not detected: %v", counter)
	}

	// Method.
	render := findSymbol(syms, ".Counter.render")
	if render == nil || render.Kind != core.SymbolMethod {
		t.Errorf("Counter.render method not detected: %v", render)
	}

	// Inheritance: Counter extends React.Component.
	inheritEdge := findEdge(edges, "Counter", "React.Component", core.EdgeInherit)
	if inheritEdge == nil {
		t.Errorf("Counter→React.Component inherit edge not detected")
	}

	// Constant.
	title := findSymbol(syms, ".TITLE")
	if title == nil || title.Kind != core.SymbolConstant {
		t.Errorf("TITLE constant not detected: %v", title)
	}
}

func TestParse_TSX_Imports(t *testing.T) {
	a := &adapter{lang: "tsx", getLang: tstsx.GetLanguage}
	syms, _, err := a.Parse("src/App.tsx", []byte(tsxFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	importCount := 0
	for _, s := range syms {
		if s.Kind == core.SymbolImport {
			importCount++
		}
	}
	if importCount < 2 {
		t.Errorf("expected ≥2 import symbols; got %d", importCount)
	}
}

func TestLanguage_JS(t *testing.T) {
	a := &adapter{lang: "js", getLang: tsjs.GetLanguage}
	if a.Language() != "js" {
		t.Errorf("Language() = %q; want js", a.Language())
	}
}

func TestLanguage_TS(t *testing.T) {
	a := &adapter{lang: "ts", getLang: tsts.GetLanguage}
	if a.Language() != "ts" {
		t.Errorf("Language() = %q; want ts", a.Language())
	}
}

func TestLanguage_TSX(t *testing.T) {
	a := &adapter{lang: "tsx", getLang: tstsx.GetLanguage}
	if a.Language() != "tsx" {
		t.Errorf("Language() = %q; want tsx", a.Language())
	}
}
