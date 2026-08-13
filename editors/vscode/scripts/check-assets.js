const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");

function readJSON(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(root, relativePath), "utf8"));
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

const manifest = readJSON("package.json");
const languageConfig = readJSON("language-configuration.json");
const grammar = readJSON("syntaxes/crl.tmLanguage.json");
const snippets = readJSON("snippets/crl.json");
const packagedLicense = fs.readFileSync(path.join(root, "LICENSE"), "utf8");
const repositoryLicense = fs.readFileSync(path.join(root, "..", "..", "LICENSE"), "utf8");

assert(manifest.main === "./extension.js", "package.json must point at extension.js");
assert(
  manifest.activationEvents.includes("onLanguage:crl"),
  "package.json must activate for CRL documents"
);
assert(
  manifest.contributes.commands.some((command) => command.command === "crl.lintDocument"),
  "package.json must contribute the CRL lint command"
);
assert(languageConfig.comments.lineComment === "#", "CRL line comments must use #");
assert(grammar.scopeName === "source.crl", "TextMate grammar must use source.crl scope");
assert(
  packagedLicense === repositoryLicense,
  "packaged extension license must match the repository license"
);

const grammarText = JSON.stringify(grammar);
for (const keyword of [
  "package",
  "bundle",
  "constructor",
  "abstract",
  "extends",
  "rule",
  "target",
  "collector",
  "signal",
  "need",
  "block",
  "quorum",
  "within",
  "age",
  "of"
]) {
  assert(grammarText.includes(keyword), `TextMate grammar must cover ${keyword}`);
}

for (const name of [
  "Rule Block",
  "Constructor",
  "Abstract Rule",
  "Collector With Schema",
  "Temporal Need",
  "N of M Quorum",
  "Cluster",
  "Block Predicate"
]) {
  assert(snippets[name], `missing snippet: ${name}`);
}

console.log("CRL language-support assets: ok");
