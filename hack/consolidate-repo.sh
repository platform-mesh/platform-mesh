#!/usr/bin/env bash
#
# consolidate-repo.sh — import an external repo into this monorepo while
# preserving meaningful (real-user) history and excluding renovate noise.
#
# ============================================================================
# HOW IT WORKS
# ============================================================================
#
# The problem: we want the per-commit history of an operator repo to live in
# this monorepo, but (a) under a subdirectory, (b) without the ~75% of commits
# that are renovate[bot] dependency bumps, and (c) without breaking the build.
#
# Three stages:
#
#   1. FRESH CLONE
#      git-filter-repo rewrites history destructively, so we never touch the
#      original — we clone the SOURCE (URL or local path) into a temp dir and
#      operate only on that throwaway copy. `--ref` pins it to a specific
#      commit (useful when the real tip is ahead of origin/main).
#
#   2. REWRITE HISTORY  (git-filter-repo)
#      a) --to-subdirectory-filter moves EVERY path in EVERY commit under
#         operators/<NAME>/ . This is what makes `git log -- operators/<NAME>`
#         find the imported commits afterwards.
#      b) A --commit-callback clears the file changes of any commit authored by
#         renovate[bot]; combined with --prune-empty=always the now-empty
#         commit is dropped. The dependency state itself is NOT lost: in graft
#         mode the monorepo's own files are authoritative, and renovate's bumps
#         were already baked into them.
#
#   3. GRAFT INTO THE MONOREPO
#      Two strategies, chosen with --mode:
#
#      * graft (default) — `git merge -s ours --allow-unrelated-histories`.
#        The "ours" strategy keeps the monorepo's CURRENT tree exactly as-is
#        and records the imported history merely as a second parent. Result:
#        not a single file changes (the script asserts the tree hash is
#        identical before/after), yet every real-user commit becomes reachable.
#        Use when the files are ALREADY in the monorepo (e.g. a flat copy that
#        was committed without history) and you only want the history attached.
#        Trade-off: `git blame` still points at the flat-copy commit; the full
#        history is visible via `git log --full-history -- operators/<NAME>`
#        (the flag is needed because -s ours makes the path TREESAME to the
#        first parent, so default log simplification hides it).
#
#      * subtree — a real `git merge --allow-unrelated-histories`. The files
#        come FROM the imported (already path-prefixed) history, so `git blame`
#        and `git log --follow` work perfectly. Use when the target path does
#        NOT yet exist in the monorepo. Refuses to run if it already does.
#
# ============================================================================
#
# Requirements: git, git-filter-repo  (brew install git-filter-repo)
#
# Examples:
#   # graft history under operators/account-operator (files already in repo):
#   hack/consolidate-repo.sh https://github.com/platform-mesh/account-operator account-operator
#
#   # from a local clone, pinned to a specific ref:
#   hack/consolidate-repo.sh ~/go/src/.../security-operator security-operator --ref 469a8c4
#
#   # bring in files AND history (path must not exist yet):
#   hack/consolidate-repo.sh <src> my-operator --mode subtree
#
set -euo pipefail

# Print the leading comment block (everything between the shebang and the first
# non-comment line) as help text — robust to edits above.
usage() {
  awk 'NR==1{next} /^#/{sub(/^# ?/,""); print; next} {exit}' "$0"
  exit "${1:-0}"
}

# ---- args -------------------------------------------------------------------
SOURCE=""
NAME=""
REF=""
PREFIX=""              # defaults to operators/<NAME>
MODE="graft"           # graft | subtree
KEEP_RENOVATE="false"

POSITIONAL=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)        usage 0 ;;
    --ref)            REF="$2"; shift 2 ;;
    --prefix)         PREFIX="$2"; shift 2 ;;
    --mode)           MODE="$2"; shift 2 ;;
    --keep-renovate)  KEEP_RENOVATE="true"; shift ;;
    -*)               echo "unknown flag: $1" >&2; usage 1 ;;
    *)                POSITIONAL+=("$1"); shift ;;
  esac
done
set -- "${POSITIONAL[@]:-}"
SOURCE="${1:-}"
NAME="${2:-}"

[[ -z "$SOURCE" || -z "$NAME" ]] && { echo "error: SOURCE and NAME are required" >&2; usage 1; }
[[ "$MODE" != "graft" && "$MODE" != "subtree" ]] && { echo "error: --mode must be graft|subtree" >&2; exit 1; }
command -v git-filter-repo >/dev/null 2>&1 || { echo "error: git-filter-repo not found (brew install git-filter-repo)" >&2; exit 1; }

PREFIX="${PREFIX:-operators/$NAME}"
MONO="$(git rev-parse --show-toplevel)"

# ---- guard rails ------------------------------------------------------------
# We're about to add merge commits — make sure we're on a branch, not detached.
if ! git -C "$MONO" symbolic-ref -q HEAD >/dev/null; then
  echo "error: monorepo is in detached HEAD. Create a branch first:" >&2
  echo "       git checkout -b consolidate-operators" >&2
  exit 1
fi

if [[ "$MODE" == "subtree" && -e "$MONO/$PREFIX" ]]; then
  echo "error: --mode subtree but '$PREFIX' already exists. Remove it first, or use graft mode." >&2
  exit 1
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/consolidate.XXXXXX")"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

echo ">> [1/4] cloning $SOURCE"
git clone --quiet "$SOURCE" "$TMP/src"
git -C "$TMP/src" checkout --quiet -B import "${REF:-HEAD}"

BEFORE=$(git -C "$TMP/src" rev-list --count HEAD)

echo ">> [2/4] rewriting history: prefix=$PREFIX drop-renovate=$([[ $KEEP_RENOVATE == true ]] && echo no || echo yes)"
CB=':'
[[ "$KEEP_RENOVATE" == "false" ]] && \
  CB='if b"renovate" in commit.author_name.lower(): commit.file_changes = []'
git -C "$TMP/src" filter-repo --force --refs import \
  --to-subdirectory-filter "$PREFIX" \
  --prune-empty=always --prune-degenerate=always \
  --commit-callback "$CB"

AFTER=$(git -C "$TMP/src" rev-list --count HEAD)
echo "   commits: $BEFORE -> $AFTER  (dropped $((BEFORE - AFTER)))"
RENLEFT=$(git -C "$TMP/src" log --author=renovate --oneline | wc -l | tr -d ' ')
[[ "$KEEP_RENOVATE" == "false" && "$RENLEFT" != "0" ]] && echo "   WARN: $RENLEFT renovate commits remain" >&2

echo ">> [3/4] grafting into $(git -C "$MONO" branch --show-current) (mode=$MODE)"
TREE_BEFORE=$(git -C "$MONO" rev-parse HEAD^{tree})
git -C "$MONO" fetch --quiet "$TMP/src" import:refs/remotes/_import/$NAME

if [[ "$MODE" == "graft" ]]; then
  git -C "$MONO" merge -s ours --allow-unrelated-histories --no-edit \
    -m "Graft $NAME history (real-user commits$([[ $KEEP_RENOVATE == false ]] && echo ', renovate excluded'))" \
    "_import/$NAME"
  TREE_AFTER=$(git -C "$MONO" rev-parse HEAD^{tree})
  if [[ "$TREE_BEFORE" == "$TREE_AFTER" ]]; then
    echo "   OK: working tree unchanged (graft only)"
  else
    echo "   ERROR: tree changed under graft mode — unexpected" >&2; exit 1
  fi
else
  # subtree: tree comes from history; paths were already prefixed by filter-repo
  git -C "$MONO" merge --allow-unrelated-histories --no-edit \
    -m "Import $NAME with history (real-user commits$([[ $KEEP_RENOVATE == false ]] && echo ', renovate excluded'))" \
    "_import/$NAME"
  echo "   OK: files imported under $PREFIX with history"
fi

git -C "$MONO" update-ref -d "refs/remotes/_import/$NAME" || true

echo ">> [4/4] done. verify with:"
echo "   git -C $MONO log --full-history --oneline -- $PREFIX"
echo "   git -C $MONO log --full-history --author=renovate --oneline -- $PREFIX   # expect empty"
