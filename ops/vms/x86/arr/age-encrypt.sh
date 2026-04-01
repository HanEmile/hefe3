set -euo pipefail

RECIPIENTS="${1:-$HOME/.ssh/id_ed25519.pub}"

find . -name 'private.nix' | while read -r f; do
    age -R "$RECIPIENTS" -o "$f.age" "$f"
    echo "encrypted to ${f}.age"
done

age -R "$RECIPIENTS" -o "notes.md.age" "notes.md"
echo "encrypted to notes.md.age"

age -R "$RECIPIENTS" -o "config.ini.age" "config.ini"
echo "encrypted to config.ini.age"
