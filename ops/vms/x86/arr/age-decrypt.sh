set -euo pipefail
IDENTITY="${AGE_IDENTITY:-$HOME/.ssh/id_ed25519}"

find . -name "*.nix.age" | while read -r f; do
    out="${f%.age}"
    if [ ! -f $out ] || [ "$f" -nt $out ]; then
       age -d -i "$IDENTITY" -o "$out" "$f"
       echo "decrypted to $out"
    fi
done

age -d -i "$IDENTITY" -o "notes.md" "notes.md.age"
echo "decrypted to notes.md"

age -d -i "$IDENTITY" -o "config.ini" "config.ini.age"
echo "decrypted to config.ini"
