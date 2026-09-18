Write-Host "==> Cerrando port-forwards del proyecto (api-service/keycloak)..." -ForegroundColor Yellow
Get-CimInstance Win32_Process -Filter "Name like 'kubectl%'" |
    Where-Object { $_.CommandLine -match 'port-forward svc/(api-service|keycloak)' } |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }

Write-Host "==> Eliminando cluster de kind (tarea-cluster)..." -ForegroundColor Yellow
$clusters = kind get clusters 2>$null
if ($clusters -contains "tarea-cluster") {
    kind delete cluster --name tarea-cluster
} else {
    Write-Host "    (no existe el cluster tarea-cluster)" -ForegroundColor Gray
}

Write-Host "==> Bajando contenedores, redes y volúmenes de Compose..." -ForegroundColor Yellow
docker compose down -v --remove-orphans 2>$null

Write-Host "==> Eliminando imágenes locales del proyecto..." -ForegroundColor Yellow
docker rmi -f tareaunobd2-api tareaunobd2-db tareaunobd2-keycloak 2>$null

Write-Host "==> Eliminando datos locales de PostgreSQL (data/) y artefactos de build..." -ForegroundColor Yellow
Remove-Item -Recurse -Force .\data -ErrorAction SilentlyContinue
Remove-Item -Force .\api\main -ErrorAction SilentlyContinue

Write-Host "Listo. Sistema limpio." -ForegroundColor Green