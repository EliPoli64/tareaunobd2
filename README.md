# Tarea Uno BD2: Reservas con Docker Compose y Kubernetes

Servicio HTTP en Go (Gin) para gestionar reservas sobre PostgreSQL, con autenticación
delegada en Keycloak (OIDC/JWT). El sistema se orquesta con **Docker Compose** y,
adicionalmente, se despliega sobre **Kubernetes (kind)** con **Kustomize**.

## Requisitos previos

- Docker con Docker Compose (v2) y el plugin Buildx.
- `kind` y `kubectl` (solo para el módulo de Kubernetes).
- Go 1.22+ (solo para ejecutar las pruebas locales).
- `curl` para probar las rutas.

## Configuración inicial

```bash
cp .env.example .env
```

El `.env` real está ignorado por Git (`.gitignore`); en el repositorio solo se versiona
`.env.example` con valores de ejemplo. Las credenciales se inyectan por variables de
entorno y **nunca** quedan dentro de la imagen.

---

## Núcleo: Docker Compose

### Arranque con un solo comando

```bash
docker compose up --build
```

Esto construye las imágenes del servicio, la base y el proveedor (`api`, `db`,
`keycloak`), las levanta junto al servicio `migrator` y crea el esquema de la base
automáticamente. La aplicación arranca solo cuando la base y el proveedor están sanos y
después de que `migrator` haya terminado su trabajo (`depends_on` con `condition`).

Servicios y puertos:

| Servicio   | Imagen                              | Puerto |
|------------|-------------------------------------|--------|
| `api`      | `./api` (Dockerfile multi-stage)    | 1412   |
| `db`       | `./db` (base `postgres:18.6-alpine3.24`) | 5432 |
| `keycloak` | `./id-provider` (base `quay.io/keycloak/keycloak:26.0.6`) | 6767 |
| `migrator` | `postgres:18.6-alpine3.24` (servicio de aplicación) |: |

El esquema se crea de dos formas complementarias: el Dockerfile de `db` copia
`db/migrations/*.sql` a `/docker-entrypoint-initdb.d` (inicializa un volumen nuevo) y,
además, el servicio `migrator` los ejecuta explícitamente con `psql` contra la base, por
lo que el esquema también queda listo si el volumen de datos ya existía. `api` no se da
por listo hasta que la base y Keycloak están sanos **y** `migrator` terminó con éxito
(`service_completed_successfully`).

Cada contenedor alcanza a los demás **por nombre de servicio** (`api`, `db`, `keycloak`)
dentro de la red que crea Compose, nunca por IP ni por `localhost`.

### Contrato de las rutas

Base: `http://localhost:1412`

| Método | Ruta                 | Auth | Respuesta |
|--------|----------------------|------|-----------|
| GET    | `/health`            |:    | 200 `{"status":"OK"}` sin tocar la base |
| GET    | `/ready`             |:    | 200 `{"status":"OK"}` si PostgreSQL responde; 503 si no |
| GET    | `/reservas`          |:    | 200 lista de reservas; admite `?fecha=AAAA-MM-DD` como filtro |
| GET    | `/reservas/{id}`     |:    | 200 con la reserva; 404 si no existe |
| POST   | `/reservas`          | sí*  | 201 con la reserva creada (incluye `reservaId`); 400 cuerpo inválido |
| PUT    | `/reservas/{id}`     | sí*  | 200 con la versión actualizada; 400 inválido; 404 si no existe |
| DELETE | `/reservas/{id}`     | sí*  | 204 sin cuerpo; 404 si no existe |

\* Rutas protegidas: sin token **401**; token válido sin rol `reserva-writer` **403**.
Listar y consultar son públicas por decisión del grupo.

Cuerpo de una reserva (POST/PUT):

```json
{ "nombre": "Ana", "fecha": "2026-09-18", "cantidadPersonas": 3, "estado": "confirmada" }
```

### Autenticación (Keycloak)

El realm `tareauno` se importa al arrancar (`id-provider/realm.json`) con el client
`api` (público, `directAccessGrants`) y dos usuarios:

| Usuario | Contraseña | Rol |
|---------|-----------|-----|
| `writer`| `writer123`| `reserva-writer` |
| `reader`| `reader123`| sin rol |

Obtener un token:

```bash
curl -s -X POST http://localhost:6767/realms/tareauno/protocol/openid-connect/token \
  -d 'client_id=api&username=writer&password=writer123&grant_type=password' \
  | jq -r .access_token
```

Usarlo en una ruta protegida:

```bash
curl -X POST http://localhost:1412/reservas \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"nombre":"Ana","fecha":"2026-09-18","cantidadPersonas":3,"estado":"confirmada"}'
```

El servicio valida firma RS256 contra el JWKS de Keycloak (`KC_JWKS_URL`, por el nombre
interno `keycloak`) y el emisor (`KC_ISSUER_URL`, el host externo `localhost`, que es el
`iss` que firman los tokens). No se implementa login ni firma propios.

### Comprobar la persistencia

```bash
docker compose down          # no usa -v: el volumen se conserva
docker compose up --build    # se vuelve a levantar
curl http://localhost:1412/reservas   # los datos escritos antes siguen ahí
```

Los datos viven en el volumen nombrado `pgdata` declarado en `docker-compose.yml`.

### Pruebas unitarias y de integración

```bash
./run-tests.sh
```

Levanta la pila real, ejecuta las unitarias (`go test ./...`) y las de integración
(`go test -tags integration ./...`) que ejercitan el CRUD, la validación y la
aceptación/rechazo de tokens contra Keycloak. Todas deben pasar.

### Apagado

```bash
docker compose down          # apaga sin borrar datos
docker compose down -v       # apaga y borra el volumen (datos perdidos)
```

---

## Kubernetes con kind y Kustomize

### Comando exacto

```bash
./run-k8s
```

El script, de principio a fin, sin pasos manuales:

1. `docker compose build --no-cache --provenance=false --sbom=false`: construye
   imágenes de un solo manifest (sin attestations) desde el working tree, para que
   `kind load` las importe de forma fiable.
2. `kind create cluster --name tarea-cluster`.
3. `kind load docker-image` de `tareaunobd2-api` y `tareaunobd2-keycloak`, verificando
   con `crictl images` que quedaron en el nodo (aborta si no).
4. `kubectl apply -k k8s/overlay/dev`.
5. `kubectl rollout status` de `api`, `db` y `keycloak` (espera real, no un `sleep`).
6. Abre los puertos con `kubectl port-forward`: **1412** (API) y **6767** (Keycloak),
   ambos hasta que se presione Ctrl+C.

Al terminar, el clúster queda listo con la API en `http://localhost:1412` y Keycloak
en `http://localhost:6767`.

### Verificación en el clúster

Con los port-forwards del paso 6 activos:

```bash
curl http://localhost:1412/health        # 200
curl http://localhost:1412/ready         # 200 si la base responde
curl http://localhost:6767/realms/tareauno/.well-known/openid-configuration | jq .issuer
```

El flujo autenticado (obtener token en `localhost:6767`, usarlo contra las rutas
protegidas de `localhost:1412`) repite el contrato de rutas de la sección del núcleo:
crear, listar, consultar, actualizar, eliminar; 401 sin token, 403 sin rol, 400 inválido,
404 inexistente.

### Correspondencia Compose <-> Kubernetes

| Docker Compose                       | Kubernetes                                |
|--------------------------------------|-------------------------------------------|
| `api` (build + ports)                | `Deployment/api` + `Service/api-service`  |
| `db` (postgres + volumen)            | `Deployment/db` + `Service/db` + `PersistentVolumeClaim/postgres-pvc` |
| `keycloak` (imagen + realm)          | `Deployment/keycloak` + `Service/keycloak`|
| Red de Compose (nombre de servicio)  | DNS interno del clúster (`api`, `db`, `keycloak`) |
| Volumen de datos (`pgdata`)          | `PersistentVolumeClaim` con el StorageClass `standard` de kind |
| `.env` / `environment`               | `ConfigMap/app-config` + `Secret/app-secrets` |
| `healthcheck`                        | `livenessProbe` y `readinessProbe` sobre `/health` y `/ready` |
| `ports` desde el host                | `Service` de tipo `NodePort` (`api-service`, nodePort 31412) |
| `depends_on` con condición           | Probes de readiness (la app no está Ready sin sus dependencias) |
| `./run-k8s` (orquestación)           | `Kustomize`: `k8s/base/` + `k8s/overlay/dev/` |

`base/` contiene los manifiestos comunes; `overlay/dev/` los personaliza sin duplicar:
sube las réplicas del API de 1 a 2 (`replica-patch.yaml`). Las migraciones SQL se
inyectan como `ConfigMap` (`db-init-scripts`) montado en `/docker-entrypoint-initdb.d`.

### Apagado

```bash
kind delete cluster --name tarea-cluster
```

### Limpieza total (clúster + contenedores + volúmenes + imágenes)

```bash
./cleanup.sh        # Linux/macOS
.\cleanup.ps1       # Windows
```

Cierra los port-forwards, borra el clúster de kind, baja los contenedores de Compose
con sus volúmenes, elimina las imágenes locales del proyecto (`tareaunobd2-*`) y los
datos de PostgreSQL (`data/`). Deja el entorno como recién clonado.

---

## Decisiones técnicas

- **Autenticación en rutas GET:** no es necesaria. Decidimos autenticar solamente cuando se
  realizan requests POST, DELETE y PUT, para que las requests de GET fueran sencillas de
  comprobar al iniciar el desarrollo del proyecto.
- **Imagen del API (multi-stage):** se compila con `golang:1.27.0-alpine3.24` y la imagen
  final solo contiene el binario (`CGO_ENABLED=0`) sobre `alpine:3.24`: ni fuentes, ni
  pruebas, ni toolchain de Go. El `.dockerignore` excluye `.env`, `.env.local`, `.git`,
  `.gitignore`, el README y las carpetas `data/`, `db/` y `k8s/`, de modo que el contexto
  de construcción tampoco lleva secretos ni historial de Git.
- **Comunicación:** por nombre de servicio en la red de Compose / DNS del clúster.
- **Configuración:** todo por variables de entorno; sin secretos en la imagen ni en el
  repositorio (solo `.env.example`).
- **Arranque ordenado:** healthchecks declarados en Compose y `depends_on` con
  `condition: service_healthy` / `service_completed_successfully`; en Kubernetes, las
  probes de readiness impiden dar por lista la app antes que sus dependencias.