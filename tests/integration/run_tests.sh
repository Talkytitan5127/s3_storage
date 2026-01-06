#!/bin/bash
# Convenient test runner script for integration tests

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
SKIP_SLOW=false
PARALLEL=false
WORKERS=4
TEST_SUITE="all"
VERBOSE=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-slow)
            SKIP_SLOW=true
            shift
            ;;
        --parallel)
            PARALLEL=true
            shift
            ;;
        --workers)
            WORKERS="$2"
            shift 2
            ;;
        --suite)
            TEST_SUITE="$2"
            shift 2
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --skip-slow       Skip slow tests (large files, benchmarks)"
            echo "  --parallel        Run tests in parallel"
            echo "  --workers N       Number of parallel workers (default: 4)"
            echo "  --suite SUITE     Run specific test suite: e2e, grpc, concurrent, all (default: all)"
            echo "  --verbose, -v     Verbose output"
            echo "  --help, -h        Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                          # Run all tests"
            echo "  $0 --skip-slow              # Skip slow tests"
            echo "  $0 --suite e2e              # Run only E2E tests"
            echo "  $0 --parallel --workers 8   # Run tests in parallel with 8 workers"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║         S3 Storage Integration Test Runner                ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Check if Docker Compose is running
echo -e "${YELLOW}🔍 Checking Docker Compose services...${NC}"
if ! docker-compose -f "$PROJECT_ROOT/docker-compose.test.yml" ps | grep -q "Up"; then
    echo -e "${YELLOW}⚠️  Services not running. Starting Docker Compose...${NC}"
    cd "$PROJECT_ROOT"
    docker-compose -f docker-compose.test.yml up -d --build
    echo -e "${YELLOW}⏳ Waiting for services to be ready (30s)...${NC}"
    sleep 30
else
    echo -e "${GREEN}✅ Services are running${NC}"
fi

# Check API health
echo -e "${YELLOW}🏥 Checking API Gateway health...${NC}"
if curl -sf http://localhost:8080/health > /dev/null; then
    echo -e "${GREEN}✅ API Gateway is healthy${NC}"
else
    echo -e "${RED}❌ API Gateway is not responding${NC}"
    echo -e "${YELLOW}💡 Try: docker-compose -f docker-compose.test.yml logs api-gateway${NC}"
    exit 1
fi

# Generate gRPC code if needed
if [ ! -d "$SCRIPT_DIR/generated" ]; then
    echo -e "${YELLOW}🔧 Generating gRPC Python code...${NC}"
    cd "$SCRIPT_DIR"
    bash generate_grpc.sh
    echo -e "${GREEN}✅ gRPC code generated${NC}"
fi

# Build pytest command
PYTEST_CMD="pytest"

# Add verbosity
if [ "$VERBOSE" = true ]; then
    PYTEST_CMD="$PYTEST_CMD -v"
fi

# Add parallel execution
if [ "$PARALLEL" = true ]; then
    PYTEST_CMD="$PYTEST_CMD -n $WORKERS"
fi

# Add markers
if [ "$SKIP_SLOW" = true ]; then
    PYTEST_CMD="$PYTEST_CMD -m 'not slow'"
fi

# Add test suite
case $TEST_SUITE in
    e2e)
        PYTEST_CMD="$PYTEST_CMD test_e2e.py"
        ;;
    grpc)
        PYTEST_CMD="$PYTEST_CMD test_grpc.py"
        ;;
    concurrent)
        PYTEST_CMD="$PYTEST_CMD test_concurrent.py"
        ;;
    all)
        # Run all tests
        ;;
    *)
        echo -e "${RED}❌ Unknown test suite: $TEST_SUITE${NC}"
        exit 1
        ;;
esac

# Add common options
PYTEST_CMD="$PYTEST_CMD --tb=short --maxfail=5"

# Run tests
echo ""
echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                    Running Tests                          ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${YELLOW}Command: $PYTEST_CMD${NC}"
echo ""

cd "$SCRIPT_DIR"
if $PYTEST_CMD; then
    echo ""
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                  ✅ All Tests Passed                       ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
    exit 0
else
    echo ""
    echo -e "${RED}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║                  ❌ Some Tests Failed                      ║${NC}"
    echo -e "${RED}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${YELLOW}💡 Tips:${NC}"
    echo -e "  - Check logs: docker-compose -f docker-compose.test.yml logs"
    echo -e "  - Run with --verbose for more details"
    echo -e "  - Run specific suite: --suite e2e"
    exit 1
fi