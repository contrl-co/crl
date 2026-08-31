//go:build js && wasm

// crl-wasm is the CRL toolchain built for the browser: the compiler,
// formatter, linter, graph projection, and evaluator, compiled to
// WebAssembly and installed as JS globals so a page can compile and
// evaluate CRL with no server round trip.
//
//	contrlCompileCRL(requestJSON)  -> responseJSON
//	contrlFormatCRL(requestJSON)   -> responseJSON
//	contrlLintCRL(requestJSON)     -> responseJSON
//	contrlGraphCRL(requestJSON)    -> responseJSON
//	contrlEvaluateCRL(requestJSON) -> responseJSON
//	contrlEngineInfo(requestJSON)  -> responseJSON
//
// Every global takes one JSON string and returns one JSON string;
// failures return {"error":"..."} rather than throwing, because a
// WebAssembly export has no error channel. See docs/wasm.md for the
// request and response shapes.
//
// What ships here is only what is already published: this command links
// the public crl API and nothing else. Build it with
// scripts/build-wasm.sh.
package main

import (
	"syscall/js"

	"github.com/contrl-co/crl/internal/crlwasm"
)

// version is stamped at build time via -ldflags "-X main.version=...",
// the same way crlc is stamped, so a page can report which toolchain
// produced a hash.
var version = "dev"

func main() {
	engine := crlwasm.Engine{Version: version}
	global := js.Global()
	for _, function := range engine.Functions() {
		call := function.Call
		global.Set(function.Name, js.FuncOf(func(_ js.Value, args []js.Value) any {
			// One JSON string in. A caller that passes an object gets a
			// named error instead of js.Value.String()'s "<object>"
			// placeholder failing later as a parse error.
			request := "{}"
			if len(args) > 0 {
				if args[0].Type() != js.TypeString {
					return js.ValueOf(`{"error":"request must be a JSON string"}`)
				}
				request = args[0].String()
			}
			return js.ValueOf(call(request))
		}))
	}
	// A Go wasm module exits when main returns, taking the exported
	// functions with it. Block forever so the globals stay callable for
	// the life of the page.
	select {}
}
