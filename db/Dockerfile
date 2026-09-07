FROM postgres:18.6-alpine3.24

EXPOSE 5432

COPY migrations/01_Tabla.sql /docker-entrypoint-initdb.d/
COPY migrations/02_SP_Reservas.sql /docker-entrypoint-initdb.d/

