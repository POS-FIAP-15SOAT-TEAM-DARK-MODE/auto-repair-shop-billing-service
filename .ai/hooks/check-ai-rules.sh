#!/usr/bin/env bash
# Pre-commit hook: enforces .ai/ as the single source of truth for AI rule files.
set -euo pipefail

ERRORS=0
# Only check added/modified files — deletions are always allowed
STAGED=$(git diff --cached --name-only --diff-filter=AM)

# 1. No .mdc rule files outside .ai/ (the single Cursor reference is allowed)
BLOCKED_MDC=$(echo "$STAGED" | grep -E '\.mdc$' | grep -v '^\.ai/' | grep -v '^\.cursor/rules/ai-rules-reference\.mdc$' || true)
if [ -n "$BLOCKED_MDC" ]; then
  echo "ERROR [check-ai-rules]: .mdc rule files must live under .ai/ — move these:"
  echo "$BLOCKED_MDC" | sed 's/^/  /'
  ERRORS=$((ERRORS + 1))
fi

# 2. Block copilot-instructions.md (retired — use AGENTS.md or .ai/ instead)
COPILOT=$(echo "$STAGED" | grep 'copilot-instructions\.md' || true)
if [ -n "$COPILOT" ]; then
  echo "ERROR [check-ai-rules]: .github/copilot-instructions.md is retired — use AGENTS.md or .ai/ instead:"
  echo "$COPILOT" | sed 's/^/  /'
  ERRORS=$((ERRORS + 1))
fi

# 3. Files in .claude/commands/ must be symlinks pointing to ../../.ai/commands/*
COMMAND_FILES=$(echo "$STAGED" | grep '^\.claude/commands/' || true)
for f in $COMMAND_FILES; do
  # git stores symlinks as blobs with mode 120000
  MODE=$(git ls-files --stage "$f" 2>/dev/null | awk '{print $1}')
  if [ "$MODE" != "120000" ]; then
    echo "ERROR [check-ai-rules]: $f is a real file — .claude/commands/ only allows symlinks to ../../.ai/commands/*"
    ERRORS=$((ERRORS + 1))
    continue
  fi
  TARGET=$(git show ":$f" 2>/dev/null || true)
  if ! echo "$TARGET" | grep -qE '^../../\.ai/commands/'; then
    echo "ERROR [check-ai-rules]: $f symlink target must be ../../.ai/commands/* (got: $TARGET)"
    ERRORS=$((ERRORS + 1))
  fi
done

if [ "$ERRORS" -gt 0 ]; then
  echo ""
  echo "Tip: add rule content to .ai/rules/ and create a pointer file if needed."
  exit 1
fi
