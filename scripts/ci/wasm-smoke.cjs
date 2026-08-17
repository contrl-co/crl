// Merge gate: the WebAssembly build must actually load in a JS host and
// answer real requests. A build that compiles but cannot instantiate, or
// that instantiates but hashes differently than the native toolchain,
// would ship a browser engine that quietly disagrees with crlc.
//
// Usage: node scripts/ci/wasm-smoke.cjs [dist/wasm]
//
// Checks, in order:
//   1. the module instantiates under the shipped wasm_exec.js and
//      installs every global the engine declares;
//   2. compiling examples/permit_quorum_2of3.crl yields the hash in
//      examples/golden.txt — the browser build is the same compiler;
//   3. the example's two fact sets evaluate to AUTHORIZED and BLOCKED;
//   4. the graph arrives positioned;
//   5. a broken source comes back as diagnostics, not as a crash.

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const repo = path.resolve(__dirname, "..", "..");
const dist = path.resolve(process.argv[2] || path.join(repo, "dist", "wasm"));
const EXAMPLE = "permit_quorum_2of3.crl";

function fail(message) {
    console.error(`wasm-smoke: ${message}`);
    process.exit(1);
}

function goldenHash(name) {
    const line = fs
        .readFileSync(path.join(repo, "examples", "golden.txt"), "utf8")
        .split("\n")
        .find((row) => row.trim().endsWith(name));
    if (!line) fail(`no golden hash for ${name}`);
    return line.trim().split(/\s+/)[0];
}

function call(name, request) {
    const fn = globalThis[name];
    if (typeof fn !== "function") fail(`${name} is not installed`);
    const raw = fn(JSON.stringify(request));
    if (typeof raw !== "string") fail(`${name} returned ${typeof raw}, want a JSON string`);
    let response;
    try {
        response = JSON.parse(raw);
    } catch (err) {
        fail(`${name} returned invalid JSON: ${err.message}`);
    }
    if (response.error) fail(`${name}: ${response.error}`);
    return response;
}

async function main() {
    // The shim is a classic script that installs globalThis.Go; require
    // runs it for its side effect, the way Go's own wasm_exec_node.js does.
    require(path.join(dist, "wasm_exec.js"));
    const go = new globalThis.Go();
    const { instance } = await WebAssembly.instantiate(
        fs.readFileSync(path.join(dist, "crl.wasm")),
        go.importObject,
    );
    // go.run resolves only when the module exits, and this one blocks
    // forever on purpose so its globals stay callable.
    void go.run(instance);
    for (let attempt = 0; attempt < 50 && typeof globalThis.contrlEngineInfo !== "function"; attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 20));
    }

    const info = call("contrlEngineInfo", {});
    for (const name of info.functions) {
        if (typeof globalThis[name] !== "function") fail(`${name} is declared but not installed`);
    }
    console.log(`engine   ${info.engine} ${info.version} (edition ${info.edition})`);
    console.log(`globals  ${info.functions.join(", ")}`);

    const source = fs.readFileSync(path.join(repo, "examples", EXAMPLE), "utf8");
    const compiled = call("contrlCompileCRL", { source });
    const golden = goldenHash(EXAMPLE);
    if (compiled.hash !== golden) {
        fail(`${EXAMPLE} compiled to ${compiled.hash}, golden says ${golden}`);
    }
    console.log(`compile  ${compiled.hash}  (matches examples/golden.txt)`);

    // The native toolchain, run on the same source, must agree with the
    // browser build byte for byte — the whole determinism claim.
    const native = JSON.parse(
        execFileSync("go", ["run", "./cmd/crlc", "compile", "-format", "json", path.join("examples", EXAMPLE)], {
            cwd: repo,
            encoding: "utf8",
        }),
    );
    if (native.hash !== compiled.hash) fail(`crlc says ${native.hash}, wasm says ${compiled.hash}`);
    if (native.canonical_text !== compiled.canonical_text) fail("canonical text differs between crlc and the wasm build");
    console.log("agree    crlc and crl.wasm produce identical canonical text and hash");

    const expectations = [
        ["permit_quorum_2of3.authorized.json", "AUTHORIZED", true],
        ["permit_quorum_2of3.blocked.json", "BLOCKED", false],
    ];
    for (const [file, want, authorized] of expectations) {
        const facts = JSON.parse(fs.readFileSync(path.join(repo, "examples", "facts", file), "utf8"));
        const evaluation = call("contrlEvaluateCRL", { source, facts, now: "2026-06-02T00:00:00Z" });
        if (evaluation.result !== want) fail(`${file} evaluated to ${evaluation.result}, want ${want}`);
        if (evaluation.authorized !== authorized) fail(`${file} authorized = ${evaluation.authorized}`);
        if (evaluation.hash !== golden) fail(`${file} evaluated a bundle with hash ${evaluation.hash}`);
        if (!Array.isArray(evaluation.checks) || evaluation.checks.length === 0) fail(`${file} returned no checks`);
        console.log(`evaluate ${want.padEnd(10)} at ${evaluation.evaluated_at}  (${evaluation.checks.length} checks)`);
    }

    // No clock in the request: freshness cannot be proven against the
    // host's own time for a fact observed in the past, so this must not
    // authorize. Fail-closed is the property, not the exact outcome.
    const hostClock = call("contrlEvaluateCRL", {
        source,
        facts: JSON.parse(fs.readFileSync(path.join(repo, "examples", "facts", "permit_quorum_2of3.authorized.json"), "utf8")),
    });
    if (hostClock.authorized) fail("stale evidence authorized against the host clock");
    console.log(`clock    ${hostClock.result} at the host clock ${hostClock.evaluated_at}`);

    const graph = call("contrlGraphCRL", { source });
    const positioned = graph.graph.nodes.filter((node) => node.width > 0 && node.height > 0);
    if (positioned.length !== graph.graph.nodes.length) fail("some graph nodes arrived without geometry");
    if (!graph.graph.edges.every((edge) => edge.points.length >= 2)) fail("some graph edges arrived without a route");
    console.log(
        `graph    ${graph.graph.nodes.length} nodes, ${graph.graph.edges.length} edges, ` +
            `${graph.graph.width}x${graph.graph.height}`,
    );

    const lint = call("contrlLintCRL", { path: "broken.crl", source: "crl v1\nbundle broken\n" });
    if (lint.ok || !lint.diagnostics || lint.diagnostics.length === 0) fail("a broken source linted clean");
    const first = lint.diagnostics[0];
    console.log(`lint     ${first.path}:${first.line}:${first.column} ${first.severity} ${first.code}: ${first.message}`);

    const formatted = call("contrlFormatCRL", { source });
    if (call("contrlCompileCRL", { source: formatted.formatted }).hash !== golden) {
        fail("formatted text does not compile back to the same hash");
    }
    console.log("format   canonical text recompiles to the same hash");

    console.log("wasm-smoke: ok");
    process.exit(0);
}

main().catch((err) => fail(err.stack || String(err)));
