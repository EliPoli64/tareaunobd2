-- 02_SP_Reservas.sql
-- Stored procedures del dominio Reserva.
-- Convenciones:
--   * insertarReserva retorna el reservaId generado (RETURNING), no un codigo 0/1.
--     Si algo falla, el error se propaga al API (que responde 500) en vez de
--     tragarse con un NOTICE, para no devolver un 201 falso.
--   * obtenerReservas es el SP general: cada parametro NULL = filtro ignorado.
--   * Los SPs por campo (fecha, estado, nombre, cantidad, id) delegan en el
--     general pasando NULL en el resto, para no duplicar la logica de filtrado.
--   * Todos los SELECT devuelven SETOF Reserva (mismas columnas de la tabla).

CREATE OR REPLACE FUNCTION insertarReserva(
  p_nombre VARCHAR(255),
  p_fecha DATE,
  p_cantidad_personas INTEGER,
  p_estado VARCHAR(31)
) RETURNS INTEGER AS $$
DECLARE
  v_id INTEGER;
BEGIN
  INSERT INTO Reserva (nombre, fecha, cantidadPersonas, estado)
  VALUES (p_nombre, p_fecha, p_cantidad_personas, p_estado)
  RETURNING reservaId INTO v_id;
  RETURN v_id;
END;
$$ LANGUAGE plpgsql;

-- SP general: filtros opcionales, NULL = no filtrar por ese campo.
CREATE OR REPLACE FUNCTION obtenerReservas(
  p_fecha DATE DEFAULT NULL,
  p_estado VARCHAR(31) DEFAULT NULL,
  p_nombre VARCHAR(255) DEFAULT NULL,
  p_cantidad_personas INTEGER DEFAULT NULL
) RETURNS SETOF Reserva AS $$
BEGIN
  RETURN QUERY
  SELECT *
  FROM Reserva
  WHERE (p_fecha IS NULL OR Reserva.fecha = p_fecha)
    AND (p_estado IS NULL OR Reserva.estado = p_estado)
    AND (p_nombre IS NULL OR Reserva.nombre ILIKE '%' || p_nombre || '%')
    AND (p_cantidad_personas IS NULL OR Reserva.cantidadPersonas = p_cantidad_personas)
  ORDER BY Reserva.reservaId;
END;
$$ LANGUAGE plpgsql;

-- SPs individuales por campo (uno por cada filtro del GET /reservas).

CREATE OR REPLACE FUNCTION obtenerReservasPorFecha(
  p_fecha DATE
) RETURNS SETOF Reserva AS $$
BEGIN
  RETURN QUERY SELECT * FROM obtenerReservas(p_fecha, NULL, NULL, NULL);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION obtenerReservasPorEstado(
  p_estado VARCHAR(31)
) RETURNS SETOF Reserva AS $$
BEGIN
  RETURN QUERY SELECT * FROM obtenerReservas(NULL, p_estado, NULL, NULL);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION obtenerReservasPorNombre(
  p_nombre VARCHAR(255)
) RETURNS SETOF Reserva AS $$
BEGIN
  RETURN QUERY SELECT * FROM obtenerReservas(NULL, NULL, p_nombre, NULL);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION obtenerReservasPorCantidad(
  p_cantidad_personas INTEGER
) RETURNS SETOF Reserva AS $$
BEGIN
  RETURN QUERY SELECT * FROM obtenerReservas(NULL, NULL, NULL, p_cantidad_personas);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION obtenerReservaPorId(
  p_reserva_id INTEGER
) RETURNS SETOF Reserva AS $$
BEGIN
  RETURN QUERY
  SELECT *
  FROM Reserva
  WHERE Reserva.reservaId = p_reserva_id;
END;
$$ LANGUAGE plpgsql;

-- SP de borrado: retorna TRUE si la fila existia y se borro,
-- FALSE si no existia (el API traduce a 204 / 404).
CREATE OR REPLACE FUNCTION eliminarReserva(
  p_reserva_id INTEGER
) RETURNS BOOLEAN AS $$
DECLARE
  v_borradas INTEGER;
BEGIN
  DELETE FROM Reserva
  WHERE Reserva.reservaId = p_reserva_id;
  GET DIAGNOSTICS v_borradas = ROW_COUNT;
  RETURN v_borradas > 0;
END;
$$ LANGUAGE plpgsql;
