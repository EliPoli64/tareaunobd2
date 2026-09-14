Write-Host "Iniciando despliegue completo en Kubernetes..." -ForegroundColor Cyan

Write-Host "Creando cluster de kind..." -ForegroundColor Yellow
kind create cluster --name tarea-cluster

Write-Host "Cargando imagenes al cluster..." -ForegroundColor Yellow
kind load docker-image tareaunobd2-api:latest --name tarea-cluster
kind load docker-image tareaunobd2-keycloak:latest --name tarea-cluster

Write-Host "Aplicando manifiestos de Kustomize..." -ForegroundColor Yellow
kubectl apply -k k8s/overlay/dev

Write-Host "Esperando 20 segundos a que los pods inicien..." -ForegroundColor Yellow
Start-Sleep -Seconds 20

Write-Host "Abriendo puerto 1412 para la API... Presiona Ctrl+C para detener." -ForegroundColor Green
kubectl port-forward svc/api-service 1412:1412