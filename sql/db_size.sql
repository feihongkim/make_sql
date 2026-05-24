-- 각 테이블의 행 수와 디스크 사용량 조회
SELECT
    s.name AS schema_name,
    t.name AS table_name,
    p.rows AS row_count,
    CAST(ROUND(SUM(a.total_pages) * 8 / 1024.0, 2) AS DECIMAL(18,2)) AS total_mb
FROM sys.tables t
JOIN sys.schemas s ON t.schema_id = s.schema_id
JOIN sys.indexes i ON t.object_id = i.object_id
JOIN sys.partitions p ON i.object_id = p.object_id AND i.index_id = p.index_id
JOIN sys.allocation_units a ON p.partition_id = a.container_id
WHERE i.index_id <= 1
GROUP BY s.name, t.name, p.rows
ORDER BY total_mb DESC
