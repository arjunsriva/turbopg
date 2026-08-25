# PostgreSQL 17 with pgvector (ANN) and Timescale pg_textsearch (BM25).
# pg_textsearch requires shared_preload_libraries, PostgreSQL 17+, and a
# glibc new enough for the pglayers binaries (Debian trixie / postgres:17).
FROM postgres:17

COPY --from=ghcr.io/pglayers/pgx-pgvector:17 / /
COPY --from=ghcr.io/pglayers/pgx-pg_textsearch:17 / /

RUN echo "shared_preload_libraries = 'pg_textsearch'" >> /usr/share/postgresql/postgresql.conf.sample
