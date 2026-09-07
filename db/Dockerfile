FROM postgres:18.6-alpine3.24

EXPOSE 5432

COPY migrations/Tabla.sql /docker-entrypoint-initdb.d/
COPY migrations/SP_Reservas.sql /docker-entrypoint-initdb.d/

