#!/usr/bin/env bash
set -uo pipefail

CLUSTER_NAME="tarea-cluster"

echo "==> Cerrando port-forwards del proyecto (api-service/keycloak)..."
pkill -f 'port-forward svc/(api-service|keycloak)' 2>/dev/null || true

echo "==> Eliminando cluster de kind ($CLUSTER_NAME)..."
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  kind delete cluster --name "$CLUSTER_NAME"
else
  echo "    (no existe el cluster $CLUSTER_NAME)"
fi

echo "==> Bajando contenedores, redes y volúmenes de Compose..."
docker compose down -v --remove-orphans 2>/dev/null || true

echo "==> Eliminando imágenes locales del proyecto..."
docker rmi -f tareaunobd2-api tareaunobd2-db tareaunobd2-keycloak 2>/dev/null || true

echo "==> Eliminando datos locales de PostgreSQL (data/) y artefactos de build..."
sudo rm -rf data
sudo rm -f api/main

echo "Listo. Sistema limpio."