#!/usr/bin/env bash
#
# Installs a pre-commit hook that validates staged YAML before every commit,
# so a broken GitHub Actions workflow cannot be committed and fail CI.
#
# Run once per clone:
#     ./scripts/setup-git-hooks.sh
#
# The hook uses these tools when applicable:
#   - actionlint (offline) — validates GitHub Actions workflows, including
#     expressions, events, runner labels, and action inputs.
#   - check-jsonschema (offline) — validates .gitlab-ci.yml against GitLab's
#     CI schema while the archived configuration remains in the repository.
#   - glab ci lint (online, optional) — GitLab's own authoritative validator.
#   - yamllint — general YAML syntax for every staged .yml/.yaml file.
#
# A staged CI configuration is blocked when its platform-specific validator
# is unavailable rather than being passed on YAML syntax alone.
#
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
hook="$repo_root/.git/hooks/pre-commit"

have() { command -v "$1" >/dev/null 2>&1; }

install_via() { # tool, brew-formula
  if have brew; then
    echo "installing $1 via brew ..."
    brew install "$2"
  elif have python3 && python3 -m pip --version >/dev/null 2>&1; then
    echo "installing $1 via pip --user ..."
    python3 -m pip install --user "$2"
  else
    echo "warning: could not install $1 automatically. Install it with: brew install $2"
  fi
}

# --- dependencies -----------------------------------------------------------
if ! have actionlint; then
  if have brew; then
    echo "installing actionlint via brew ..."
    brew install actionlint
  else
    echo "warning: actionlint is required for GitHub workflow changes."
    echo "         install it from https://github.com/rhysd/actionlint"
  fi
fi
have check-jsonschema || install_via check-jsonschema check-jsonschema
have yamllint         || install_via yamllint yamllint

if ! have glab; then
  echo "note: glab not installed — CI is still validated offline by check-jsonschema."
  echo "      install glab for the authoritative online check: brew install glab && glab auth login"
fi

# --- back up any unrelated existing hook ------------------------------------
mkdir -p "$(dirname "$hook")"
if [ -e "$hook" ] && ! grep -q 'crl-yaml-precommit' "$hook" 2>/dev/null; then
  backup="$hook.bak.$(date +%Y%m%d%H%M%S)"
  echo "existing pre-commit hook found; backing it up to $backup"
  mv "$hook" "$backup"
fi

# --- write the hook ---------------------------------------------------------
cat > "$hook" <<'HOOK'
#!/usr/bin/env bash
# crl-yaml-precommit — validate staged YAML so a broken CI config can't land.
# Uses actionlint for GitHub Actions, plus check-jsonschema/glab for the
# archived GitLab configuration and yamllint for general YAML syntax.
# Bypass in an emergency with:  git commit --no-verify
set -uo pipefail

staged=$(git diff --cached --name-only --diff-filter=ACM | grep -E '\.(ya?ml)$' || true)
[ -z "$staged" ] && exit 0

# This hook is only as good as the tools present. If none are installed it can
# validate nothing — say so loudly rather than pass YAML unchecked.
if ! command -v actionlint >/dev/null 2>&1 \
  && ! command -v check-jsonschema >/dev/null 2>&1 \
  && ! command -v glab >/dev/null 2>&1 \
  && ! command -v yamllint >/dev/null 2>&1; then
  echo "pre-commit: no YAML validation tool installed — nothing was checked."
  echo "            install at least one:  brew install actionlint         (GitHub Actions)"
  echo "                                    brew install check-jsonschema   (GitLab schema)"
  echo "                                    brew install yamllint          (YAML syntax)"
  echo "                                    brew install glab              (online CI validation)"
  echo "            or run: ./scripts/setup-git-hooks.sh   (bypass once with: git commit --no-verify)"
  exit 1
fi

status=0
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

for f in $staged; do
  # Validate the STAGED content, not the working tree.
  content="$tmpdir/$(printf '%s' "$f" | tr '/' '_')"
  git show ":$f" > "$content" 2>/dev/null || continue

  case "$f" in
    .github/workflows/*.yml|.github/workflows/*.yaml)
      if command -v actionlint >/dev/null 2>&1; then
        if out=$(actionlint "$content" 2>&1); then
          echo "  ok    actionlint  $f"
        else
          echo "  FAIL  actionlint  $f"
          printf '%s\n' "$out" | sed "s|$content|$f|g; s/^/        /"
          status=1
        fi
      else
        echo "  FAIL  ci-config  $f — actionlint is not installed."
        echo "        install it: brew install actionlint"
        status=1
      fi
      ;;
    .gitlab-ci.yml|*/.gitlab-ci.yml)
      validated=0

      # Primary: offline GitLab CI schema validation. No network, no auth.
      if command -v check-jsonschema >/dev/null 2>&1; then
        validated=1
        if out=$(check-jsonschema --builtin-schema vendor.gitlab-ci "$content" 2>&1); then
          echo "  ok    ci-schema  $f"
        else
          echo "  FAIL  ci-schema  $f"
          printf '%s\n' "$out" | sed "s|$content|$f|g; s/^/        /"
          status=1
        fi
      fi

      # Bonus: GitLab's authoritative validator, when installed and reachable.
      if command -v glab >/dev/null 2>&1; then
        out=$(glab ci lint "$content" 2>&1) || true
        if printf '%s' "$out" | grep -qiE 'valid!'; then
          validated=1; echo "  ok    glab-ci    $f"
        elif printf '%s' "$out" | grep -qiE 'invalid|should be'; then
          validated=1; echo "  FAIL  glab-ci    $f"
          printf '%s\n' "$out" | sed 's/^/        /'
          status=1
        else
          echo "  warn  glab-ci    $f (offline — skipped, schema check still ran)"
        fi
      fi

      # Fail closed: never let a CI config through with no schema validation.
      if [ "$validated" -eq 0 ]; then
        echo "  FAIL  ci-config  $f — no CI schema validator installed."
        echo "        install one:  brew install check-jsonschema   (offline)   or   brew install glab"
        status=1
      fi
      ;;
  esac

  # General YAML syntax for every staged YAML file.
  if command -v yamllint >/dev/null 2>&1; then
    if yamllint -d relaxed "$content" >/dev/null 2>&1; then
      echo "  ok    yamllint   $f"
    else
      echo "  FAIL  yamllint   $f"
      yamllint -d relaxed "$content" | sed "s|$content|$f|g; s/^/        /"
      status=1
    fi
  fi
done

if [ "$status" -ne 0 ]; then
  echo "pre-commit: YAML validation failed. Fix the files, or bypass with 'git commit --no-verify'."
fi
exit "$status"
HOOK

chmod +x "$hook"
echo "installed pre-commit hook at $hook"

# --- require at least one tool ----------------------------------------------
if ! have actionlint && ! have check-jsonschema && ! have glab && ! have yamllint; then
  echo
  echo "WARNING: none of actionlint / check-jsonschema / glab / yamllint are installed."
  echo "         The hook is installed but will REFUSE to validate (and block YAML"
  echo "         commits) until at least one is available. Install one:"
  echo "             brew install actionlint         # GitHub Actions (required for workflows)"
  echo "             brew install check-jsonschema   # archived GitLab CI schema"
  echo "             brew install yamllint           # YAML syntax"
  echo "             brew install glab               # online CI validation"
else
  echo "validators present:$(have actionlint && printf ' actionlint')$(have check-jsonschema && printf ' check-jsonschema')$(have glab && printf ' glab')$(have yamllint && printf ' yamllint')"
fi
