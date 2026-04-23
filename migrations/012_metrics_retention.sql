-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION prune_old_metrics_partitions(retain_months integer)
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    part record;
    dropped_count integer := 0;
    keep_from date;
    part_month date;
BEGIN
    IF retain_months IS NULL OR retain_months < 1 THEN
        RAISE EXCEPTION 'retain_months must be >= 1';
    END IF;

    keep_from := (date_trunc('month', NOW())::date - make_interval(months => retain_months - 1))::date;

    FOR part IN
        SELECT c.relname AS partition_name
        FROM pg_inherits i
        JOIN pg_class p ON p.oid = i.inhparent
        JOIN pg_class c ON c.oid = i.inhrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE p.relname = 'metrics'
          AND n.nspname = 'public'
          AND c.relname ~ '^metrics_y[0-9]{4}m[0-9]{2}$'
    LOOP
        part_month := to_date(substr(part.partition_name, 10, 4) || substr(part.partition_name, 16, 2), 'YYYYMM');
        IF part_month < keep_from THEN
            EXECUTE format('DROP TABLE IF EXISTS %I CASCADE', part.partition_name);
            dropped_count := dropped_count + 1;
        END IF;
    END LOOP;

    RETURN dropped_count;
END;
$$;
-- +goose StatementEnd

-- +goose Down

DROP FUNCTION IF EXISTS prune_old_metrics_partitions(integer);
