# run compiler 
#run abigen

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "Base dir: $SCRIPT_DIR"

solc --optimize --optimize-runs 200 \
--abi --bin "$SCRIPT_DIR/Storage.sol" -o "$SCRIPT_DIR/build"

abigen \
--abi "$SCRIPT_DIR/build/Storage.abi" \
--bin "$SCRIPT_DIR/build/Storage.bin" \
--pkg storage \
--out "$SCRIPT_DIR/storage.go"