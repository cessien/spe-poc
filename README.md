# SPE POC — Extended
Go server + deck.gl UI for sinusoidal positional encoding across H3 multi-resolution + 28-day cycles,
sqlite-vec indexing/search, RRULE support, per-day H3 heatmaps, and a VROOM simulation stub.

## Run
1. Go 1.22+ installed
2. Put a **public Mapbox token** in `config.yaml`.
3. Optional: set `storage.sqlite_vec_path` to your sqlite-vec extension path.
4. Optional: export `VROOM_BIN=/path/to/vroom` if you want real runs.
5. `go run .` and open `http://localhost:8080`

## Endpoints
- `POST /api/embed` → concatenated scenario embedding with labeled components + offsets
- `POST /api/scenario/save` → persist scenario (SQLite)
- `POST /api/index` → persist + index embedding in sqlite-vec (if available)
- `POST /api/search` → ANN cosine (fallback brute-force)
- `POST /api/heatmap` → per-day, per-feature H3 aggregation for heatmap
- `POST /api/simulate` → VROOM JSON (+optional run) and naive fallback stats; returns a 28-dim summary vector

See `web/app.jsx` for the scenario shape and UI behavior.
