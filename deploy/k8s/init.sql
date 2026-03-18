-- Go-LLM-Gateway database schema
-- Loaded by postgres docker-entrypoint-initdb.d on first start.

CREATE EXTENSION IF NOT EXISTS vector;  -- pgvector for semantic caching

-- Request log: stores every inference request for cost tracking + auditing
CREATE TABLE IF NOT EXISTS requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider        TEXT NOT NULL,
    model           TEXT NOT NULL,
    prompt_tokens   INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens    INT NOT NULL DEFAULT 0,
    latency_ms      INT NOT NULL DEFAULT 0,
    user_id         TEXT,
    request_id      TEXT,
    status          TEXT NOT NULL DEFAULT 'success', -- success | error | cached
    error_message   TEXT
);

-- Index for cost dashboards: queries like "total tokens per model per day"
CREATE INDEX IF NOT EXISTS idx_requests_created_model ON requests (created_at, model);
CREATE INDEX IF NOT EXISTS idx_requests_provider ON requests (provider);

-- Semantic cache: stores request embeddings for similarity search
-- Used in Step 5 to avoid re-running identical or near-identical prompts
CREATE TABLE IF NOT EXISTS semantic_cache (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    model           TEXT NOT NULL,
    prompt_hash     TEXT NOT NULL,             -- SHA256 of the prompt for exact matching
    prompt_embedding vector(1536),             -- OpenAI ada-002 or local embedding model
    response        JSONB NOT NULL,            -- cached ChatCompletionResponse
    hit_count       INT NOT NULL DEFAULT 0
);

-- IVFFlat index for approximate nearest-neighbour search on embeddings
-- (tune lists= to sqrt(num_rows) once you have data)
CREATE INDEX IF NOT EXISTS idx_semantic_cache_embedding
    ON semantic_cache USING ivfflat (prompt_embedding vector_cosine_ops)
    WITH (lists = 100);

CREATE INDEX IF NOT EXISTS idx_semantic_cache_model_hash ON semantic_cache (model, prompt_hash);
