#!/bin/bash
# Generate Python gRPC code from proto file

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROTO_DIR="$PROJECT_ROOT/api/proto"
OUTPUT_DIR="$SCRIPT_DIR/generated"

echo "🔧 Generating Python gRPC code..."
echo "   Proto dir: $PROTO_DIR"
echo "   Output dir: $OUTPUT_DIR"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Generate Python code
python -m grpc_tools.protoc \
    -I"$PROTO_DIR" \
    --python_out="$OUTPUT_DIR" \
    --grpc_python_out="$OUTPUT_DIR" \
    "$PROTO_DIR/storage.proto"

# Create __init__.py
touch "$OUTPUT_DIR/__init__.py"

echo "✅ Python gRPC code generated successfully"