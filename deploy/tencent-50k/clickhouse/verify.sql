SELECT database, name, engine
FROM system.tables
WHERE database = 'newapi_logs' AND name IN ('logs', 'logs_local')
ORDER BY name;

DESCRIBE TABLE newapi_logs.logs;

SELECT
    partition,
    sum(rows) AS rows,
    formatReadableSize(sum(bytes_on_disk)) AS bytes
FROM clusterAllReplicas('{cluster}', system.parts)
WHERE database = 'newapi_logs' AND table = 'logs_local' AND active
GROUP BY partition
ORDER BY partition;

