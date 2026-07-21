const cp = require("child_process");
const fs = require("fs");
const path = require("path");
const vscode = require("vscode");

const LANGUAGE_ID = "crl";
const LINT_ARGS = ["lint", "-format", "json", "-fail-on", "none"];

function activate(context) {
  const diagnostics = vscode.languages.createDiagnosticCollection("crl");
  const output = vscode.window.createOutputChannel("CRL");
  const timers = new Map();
  const runs = new Map();

  context.subscriptions.push(diagnostics, output);

  context.subscriptions.push(
    vscode.commands.registerCommand("crl.lintDocument", async () => {
      const editor = vscode.window.activeTextEditor;
      if (editor && isCRLDocument(editor.document)) {
        await lintDocument(editor.document, diagnostics, output, runs);
      }
    })
  );

  context.subscriptions.push(
    vscode.workspace.onDidOpenTextDocument((document) => {
      if (isCRLDocument(document)) {
        scheduleLint(document, diagnostics, output, timers, runs, 0);
      }
    })
  );

  context.subscriptions.push(
    vscode.workspace.onDidChangeTextDocument((event) => {
      if (!isCRLDocument(event.document)) {
        return;
      }
      const config = lintConfig(event.document);
      if (config.run === "onType") {
        scheduleLint(event.document, diagnostics, output, timers, runs, config.delayMs);
      }
    })
  );

  context.subscriptions.push(
    vscode.workspace.onDidSaveTextDocument((document) => {
      if (!isCRLDocument(document)) {
        return;
      }
      const config = lintConfig(document);
      if (config.run !== "off") {
        scheduleLint(document, diagnostics, output, timers, runs, 0);
      }
    })
  );

  context.subscriptions.push(
    vscode.workspace.onDidCloseTextDocument((document) => {
      timers.delete(document.uri.toString());
      runs.delete(document.uri.toString());
      diagnostics.delete(document.uri);
    })
  );

  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration("crl.lint")) {
        for (const document of vscode.workspace.textDocuments) {
          if (isCRLDocument(document)) {
            scheduleLint(document, diagnostics, output, timers, runs, 0);
          }
        }
      }
    })
  );

  for (const document of vscode.workspace.textDocuments) {
    if (isCRLDocument(document)) {
      scheduleLint(document, diagnostics, output, timers, runs, 0);
    }
  }
}

function deactivate() {}

function isCRLDocument(document) {
  return document.languageId === LANGUAGE_ID || document.fileName.toLowerCase().endsWith(".crl");
}

function scheduleLint(document, diagnostics, output, timers, runs, delayMs) {
  const key = document.uri.toString();
  const previous = timers.get(key);
  if (previous) {
    clearTimeout(previous);
  }
  timers.set(
    key,
    setTimeout(() => {
      timers.delete(key);
      lintDocument(document, diagnostics, output, runs);
    }, Math.max(0, delayMs || 0))
  );
}

async function lintDocument(document, diagnostics, output, runs) {
  const config = lintConfig(document);
  if (config.run === "off") {
    diagnostics.delete(document.uri);
    return;
  }

  const key = document.uri.toString();
  const runID = (runs.get(key) || 0) + 1;
  runs.set(key, runID);

  try {
    const invocation = resolveLintInvocation(document, config);
    const raw = await runLinter(invocation, document.getText(), config.trace, output);
    if (runs.get(key) !== runID) {
      return;
    }
    diagnostics.set(document.uri, diagnosticsFromReport(raw, document));
  } catch (error) {
    if (runs.get(key) !== runID) {
      return;
    }
    diagnostics.set(document.uri, [toolDiagnostic(document, error)]);
  }
}

function lintConfig(document) {
  const config = vscode.workspace.getConfiguration("crl.lint", document.uri);
  return {
    command: config.get("command", "auto"),
    args: config.get("args", []),
    run: config.get("run", "onType"),
    delayMs: config.get("delayMs", 300),
    trace: config.get("trace", false)
  };
}

function resolveLintInvocation(document, config) {
  const folder = vscode.workspace.getWorkspaceFolder(document.uri);
  const start = folder ? folder.uri.fsPath : path.dirname(document.uri.fsPath);
  const args = Array.isArray(config.args) ? config.args.slice() : [];
  const configured = String(config.command || "auto").trim();

  if (configured && configured !== "auto") {
    return {
      command: configured,
      args: args.concat(LINT_ARGS),
      cwd: start
    };
  }

  const toolchainRoot = findToolchainRoot(start);
  if (toolchainRoot) {
    return {
      command: "go",
      args: ["run", "./cmd/crlc"].concat(LINT_ARGS),
      cwd: toolchainRoot
    };
  }

  return {
    command: "crlc",
    args: LINT_ARGS,
    cwd: start
  };
}

// findToolchainRoot detects a checkout of the CRL toolchain repository
// so extension developers get diagnostics from the in-tree compiler;
// everyone else runs the installed crlc from PATH.
function findToolchainRoot(start) {
  let current = start;
  while (current && current !== path.dirname(current)) {
    if (isToolchainRoot(current)) {
      return current;
    }
    current = path.dirname(current);
  }
  return "";
}

function isToolchainRoot(candidate) {
  return (
    fs.existsSync(path.join(candidate, "go.mod")) &&
    fs.existsSync(path.join(candidate, "cmd", "crlc")) &&
    fs.existsSync(path.join(candidate, "internal", "crl"))
  );
}

function runLinter(invocation, source, trace, output) {
  if (trace) {
    output.appendLine(`$ ${invocation.command} ${invocation.args.join(" ")}`);
    output.appendLine(`cwd: ${invocation.cwd}`);
  }

  return new Promise((resolve, reject) => {
    const child = cp.spawn(invocation.command, invocation.args, {
      cwd: invocation.cwd,
      shell: false,
      windowsHide: true
    });
    let stdout = "";
    let stderr = "";

    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code !== 0 && stdout.trim() === "") {
        reject(new Error(stderr.trim() || `crlc exited with code ${code}`));
        return;
      }
      if (trace && stderr.trim()) {
        output.appendLine(stderr.trim());
      }
      resolve(stdout);
    });
    child.stdin.end(source);
  });
}

function diagnosticsFromReport(raw, document) {
  let payload;
  try {
    payload = JSON.parse(raw);
  } catch (error) {
    throw new Error(`crlc returned invalid JSON: ${error.message}`);
  }
  const reports = Array.isArray(payload.reports) ? payload.reports : [];
  const out = [];
  for (const report of reports) {
    const diagnostics = Array.isArray(report.diagnostics) ? report.diagnostics : [];
    for (const diagnostic of diagnostics) {
      out.push(toVSCodeDiagnostic(document, diagnostic));
    }
  }
  return out;
}

function toVSCodeDiagnostic(document, diagnostic) {
  const line = clamp((diagnostic.line || 1) - 1, 0, Math.max(0, document.lineCount - 1));
  const textLine = document.lineAt(line);
  const startColumn = clamp((diagnostic.column || 1) - 1, 0, textLine.text.length);
  const endColumn = Math.min(textLine.text.length, Math.max(startColumn + 1, wordEnd(textLine.text, startColumn)));
  const item = new vscode.Diagnostic(
    new vscode.Range(line, startColumn, line, endColumn),
    diagnostic.message || "CRL diagnostic",
    severityFor(diagnostic.severity)
  );
  item.code = diagnostic.code || undefined;
  item.source = "crlc";
  return item;
}

function toolDiagnostic(document, error) {
  const firstLine = document.lineCount > 0 ? document.lineAt(0).text : "";
  const endColumn = firstLine.length > 0 ? 1 : 0;
  const item = new vscode.Diagnostic(
    new vscode.Range(0, 0, 0, endColumn),
    `CRL linter unavailable: ${error.message}`,
    vscode.DiagnosticSeverity.Warning
  );
  item.code = "CRL000";
  item.source = "crlc";
  return item;
}

function severityFor(severity) {
  switch (severity) {
    case "error":
      return vscode.DiagnosticSeverity.Error;
    case "warning":
      return vscode.DiagnosticSeverity.Warning;
    case "info":
      return vscode.DiagnosticSeverity.Information;
    default:
      return vscode.DiagnosticSeverity.Hint;
  }
}

function wordEnd(text, start) {
  const match = /[A-Za-z0-9_.-]+/.exec(text.slice(start));
  if (match && match.index === 0) {
    return start + match[0].length;
  }
  return start + 1;
}

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

module.exports = {
  activate,
  deactivate,
  diagnosticsFromReport,
  resolveLintInvocation
};
