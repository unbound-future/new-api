SELECT database, name, engine
FROM system.tables
WHERE database = 'newapi_logs' AND name = 'logs'
ORDER BY name;

DESCRIBE TABLE newapi_logs.logs;

SELECT
    partition,
    sum(rows) AS rows,
    formatReadableSize(sum(bytes_on_disk)) AS bytes
FROM system.parts
WHERE database = 'newapi_logs' AND table = 'logs' AND active
GROUP BY partition
ORDER BY partition;
