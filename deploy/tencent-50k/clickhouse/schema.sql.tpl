CREATE DATABASE IF NOT EXISTS newapi_logs;

CREATE TABLE IF NOT EXISTS newapi_logs.logs
(
    id Int64 DEFAULT 0,
    user_id Int32 DEFAULT 0,
    created_at Int64 DEFAULT 0,
    type Int32 DEFAULT 0,
    content String DEFAULT '' CODEC(ZSTD(3)),
    username String DEFAULT '',
    token_name String DEFAULT '',
    model_name String DEFAULT '',
    quota Int32 DEFAULT 0,
    prompt_tokens Int32 DEFAULT 0,
    completion_tokens Int32 DEFAULT 0,
    use_time Int32 DEFAULT 0,
    is_stream UInt8 DEFAULT 0,
    channel_id Int32 DEFAULT 0,
    token_id Int32 DEFAULT 0,
    `group` String DEFAULT '',
    ip String DEFAULT '',
    request_id String DEFAULT '',
    upstream_request_id String DEFAULT '',
    other String DEFAULT '' CODEC(ZSTD(3)),
    INDEX idx_request_id request_id TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_upstream_request_id upstream_request_id TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_model_name model_name TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_token_name token_name TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_username username TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_group `group` TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_channel_id channel_id TYPE set(4096) GRANULARITY 4,
    INDEX idx_token_id token_id TYPE set(4096) GRANULARITY 4,
    INDEX idx_type type TYPE set(16) GRANULARITY 4,
    PROJECTION prj_user_history
    (
        SELECT *
        ORDER BY (user_id, created_at, request_id)
    )
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(toDateTime(created_at, 'Asia/Shanghai'))
ORDER BY (created_at, request_id)
SETTINGS index_granularity = 8192;
