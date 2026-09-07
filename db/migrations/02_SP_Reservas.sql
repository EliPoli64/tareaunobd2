CREATE OR REPLACE FUNCTION insertarReserva(
  p_nombre VARCHAR(255),
  p_fecha DATE,
  p_cantidad_personas INTEGER,
  p_estado VARCHAR(31)
) RETURNS INTEGER AS $$
BEGIN
  INSERT INTO Reserva (nombre, fecha, cantidadPersonas, estado)
  VALUES (p_nombre, p_fecha, p_cantidad_personas, p_estado);
  RETURN 0;
EXCEPTION
  WHEN OTHERS THEN
    RAISE NOTICE 'insertarReserva failed: %', SQLERRM;
    RETURN 1;
END;
$$ LANGUAGE plpgsql;