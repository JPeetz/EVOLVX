#!/usr/bin/env bash
# EvolvX — one-line installer
# Usage: curl -fsSL https://raw.githubusercontent.com/JPeetz/EvolvX/main/install.sh | bash

set -euo pipefail

REPO="JPeetz/EvolvX"
COMPOSE_FILE="docker-compose.prod.yml"

echo ""
echo "███████╗██╗   ██╗ ██████╗ ██╗     ██╗   ██╗██╗  ██╗"
echo "██╔════╝██║   ██║██╔═══██╗██║     ██║   ██║╚██╗██╔╝"
echo "█████╗  ██║   ██║██║   ██║██║     ██║   ██║ ╚███╔╝ "
echo "██╔══╝  ╚██╗ ██╔╝██║   ██║██║     ╚██╗ ██╔╝ ██╔██╗ "
echo "███████╗ ╚████╔╝ ╚██████╔╝███████╗ ╚████╔╝ ██╔╝ ██╗"
echo "╚══════╝  ╚═══╝   ╚═════╝ ╚══════╝  ╚═══╝  ╚═╝  ╚═╝"
echo ""
echo "The AI Trading OS with Memory, Discipline, and Controlled Evolution"
echo "Built on NOFX (github.com/NoFxAiOS/nofx)"
echo ""

# Check for docker
if ! command -v docker &>/dev/null; then
  echo "ERROR: Docker is required. Install it from https://docs.docker.com/get-docker/"
  exit 1
fi

if ! command -v docker compose &>/dev/null && ! command -v docker-compose &>/dev/null; then
  echo "ERROR: Docker Compose is required."
  exit 1
fi

# Download compose file
echo "Downloading docker-compose.prod.yml ..."
curl -fsSL "https://raw.githubusercontent.com/${REPO}/main/${COMPOSE_FILE}" -o "${COMPOSE_FILE}"

# Start
echo "Starting EvolvX ..."
docker compose -f "${COMPOSE_FILE}" pull
docker compose -f "${COMPOSE_FILE}" up -d

echo ""
echo "✅ EvolvX is running."
echo ""
echo "   Dashboard:  http://localhost:3000"
echo "   Registry:   http://localhost:3000/api/v1/registry/strategies"
echo "   Journal:    http://localhost:3000/api/v1/journal/decisions"
echo "   Optimizer:  http://localhost:3000/api/v1/optimizer/jobs"
echo ""
echo "Built on NOFX — please star the original: https://github.com/NoFxAiOS/nofx"
echo ""
