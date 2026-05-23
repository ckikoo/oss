#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
FAILED=0

check_frontmatter() {
  file="$1"
  fm=$(awk 'BEGIN{in=0} /^---/ {if(in==0){in=1; next} else {exit}} in==1{print}' "$file" 2>/dev/null || true)
  if [ -z "$fm" ]; then
    echo "ERROR: $file missing YAML frontmatter (--- ... ---)"
    FAILED=1
    return
  fi
  for key in name user-invocable version applyTo last_modified; do
    if ! echo "$fm" | grep -E "^[[:space:]]*$key:" >/dev/null; then
      echo "ERROR: $file frontmatter missing '$key'"
      FAILED=1
    fi
  done
}

check_memory() {
  file="$1"
  if ! grep -qE "^last_updated:" "$file" 2>/dev/null; then
    echo "ERROR: $file missing 'last_updated' field"
    FAILED=1
  fi
}

echo "Running agents frontmatter validation..."

# Skills files (search common skill folders)
while IFS= read -r -d '' f; do
  check_frontmatter "$f"
done < <(find "$ROOT_DIR/.agents/skills" -type f -name "*.md" -print0 2>/dev/null)

# Memory files
while IFS= read -r -d '' f; do
  check_memory "$f"
done < <(find "$ROOT_DIR/.agents/memory" -type f -name "*.md" -print0 2>/dev/null)

if [ "$FAILED" -ne 0 ]; then
  echo "\nValidation failed: please fix the above errors."
  exit 2
fi

echo "Validation passed: all agent skills and memories have required fields." 
exit 0
