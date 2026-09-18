#!/usr/bin/env bash
# Ejecuta las pruebas unitarias y de integración con un solo comando.
# Prerrequisito: Docker y Docker Compose instalados, y .env copiado de .env.example.
set -euo pipefail
cd "$(dirname "$0")"

echo "==> Levantando la pila con docker compose..."
docker compose up -d --build --wait

echo "==> Cargando configuración desde .env (para las pruebas de integración)..."
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

cd api

echo "==> Pruebas unitarias..."
go test ./...

echo "==> Pruebas de integración (pila real: PostgreSQL + Keycloak)..."
go test -tags integration ./...