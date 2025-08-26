package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teambition/rrule-go"
	h3 "github.com/uber/h3-go/v4"
)

// --- SQLite & sqlite-vec helpers ---

func (s *server) initDB() error {
	dbPath := s.cfg.Storage.DBPath
	if dbPath == "" { dbPath = "./data.db" }
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil { return err }
	_, _ = db.Exec(`PRAGMA journal_mode=WAL`)
	if s.cfg.Storage.SqliteVecPath != "" {
		if _, err := db.Exec(`SELECT load_extension(?)`, s.cfg.Storage.SqliteVecPath); err != nil {
			log.Printf("[warn] load sqlite-vec: %v", err)
		} else {
			log.Printf("sqlite-vec extension loaded")
		}
	}
	s.db = db
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS scenarios(id TEXT PRIMARY KEY, name TEXT, json TEXT, created_at TEXT)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS embeddings(id TEXT PRIMARY KEY, scenario_id TEXT, dim INTEGER, vector_json TEXT, created_at TEXT)`)
	return nil
}

func (s *server) ensureVecTable(dim int) error {
	if s.db == nil { return errors.New("db not initialized") }
	table := fmt.Sprintf("vec_embeddings_%d", dim)
	_, err := s.db.Exec(fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding FLOAT[%d])`, table, dim))
	if err != nil { log.Printf("[warn] vec table create failed: %v", err) }
	return nil
}

func (s *server) saveScenario(sc Scenario) (string, error) {
	if s.db == nil { return "", errors.New("db not initialized") }
	id := uuidNew()
	js, _ := json.Marshal(sc)
	_, err := s.db.Exec(`INSERT INTO scenarios(id,name,json,created_at) VALUES(?,?,?,?)`,
		id, sc.Name, string(js), time.Now().UTC().Format(time.RFC3339))
	return id, err
}

func (s *server) saveEmbedding(scenarioID string, vec []float64) (string, error) {
	if s.db == nil { return "", errors.New("db not initialized") }
	id := uuidNew()
	js, _ := json.Marshal(vec)
	_, err := s.db.Exec(`INSERT INTO embeddings(id,scenario_id,dim,vector_json,created_at) VALUES(?,?,?,?,?)`,
		id, scenarioID, len(vec), string(js), time.Now().UTC().Format(time.RFC3339))
	return id, err
}

func (s *server) indexEmbeddingVec(dim int, vec []float64) error {
	if s.db == nil { return errors.New("db not initialized") }
	table := fmt.Sprintf("vec_embeddings_%d", dim)
	ph := make([]string, dim); args := make([]any, dim)
	for i := 0; i < dim; i++ { ph[i] = "?"; args[i] = vec[i] }
	q := fmt.Sprintf(`INSERT INTO %s (embedding) VALUES (vec_f32(%s))`, table, strings.Join(ph, ","))
	_, err := s.db.Exec(q, args...)
	if err != nil { log.Printf("[warn] vec insert failed: %v", err) }
	return err
}

type SearchHit struct{ Ref string `json:"ref"`; Distance float64 `json:"distance"` }

func (s *server) searchSimilar(dim int, query []float64, k int) ([]SearchHit, error) {
	if s.db == nil { return nil, errors.New("db not initialized") }
	if k <= 0 { k = 5 }
	table := fmt.Sprintf("vec_embeddings_%d", dim)
	// try sqlite-vec
	args := make([]any, len(query)+1)
	for i := range query { args[i] = query[i] }
	args[len(query)] = k
	rows, err := s.db.Query(fmt.Sprintf(`SELECT rowid, distance_cos(embedding, vec_f32(%s)) as d FROM %s ORDER BY d ASC LIMIT ?`,
		strings.Repeat("?,", dim-1)+"?"), args...)
	if err == nil {
		defer rows.Close()
		out := []SearchHit{}
		for rows.Next() {
			var rowid int64; var d float64
			if err := rows.Scan(&rowid, &d); err == nil {
				out = append(out, SearchHit{Ref: fmt.Sprintf("%s:%d", table, rowid), Distance: d})
			}
		}
		if len(out) > 0 { return out, nil }
	}
	// fallback brute-force over embeddings
	rows2, err2 := s.db.Query(`SELECT id, vector_json FROM embeddings WHERE dim=?`, dim)
	if err2 != nil { return nil, err2 }
	defer rows2.Close()
	var buf []struct{ id string; d float64 }
	qn := l2NormalizeCopy(query)
	for rows2.Next() {
		var id, js string
		if rows2.Scan(&id, &js) == nil {
			var v []float64
			if json.Unmarshal([]byte(js), &v) == nil && len(v) == dim {
				vn := l2NormalizeCopy(v)
				d := cosineDistance(qn, vn)
				buf = append(buf, struct{ id string; d float64 }{id, d})
			}
		}
	}
	sort.Slice(buf, func(i,j int) bool { return buf[i].d < buf[j].d })
	out := []SearchHit{}
	for i := 0; i < minInt(k, len(buf)); i++ { out = append(out, SearchHit{Ref: buf[i].id, Distance: buf[i].d}) }
	return out, nil
}

func toAny(f []float64) []any { out := make([]any, len(f)); for i := range f { out[i] = f[i] }; return out }
func l2NormalizeCopy(v []float64) []float64 { out := append([]float64(nil), v...); var ss float64; for _, x := range out { ss += x*x }; if ss==0 {return out}; inv:=1.0/math.Sqrt(ss); for i:=range out{out[i]*=inv}; return out }
func cosineDistance(a,b []float64) float64 { var dot float64; for i:=range a { dot += a[i]*b[i] }; return 1-dot }
func minInt(a,b int) int { if a<b {return a}; return b }

// --- RRULE expansion ---
func expandScheduleRRULE(sch Schedule, cycleDays int) []int {
	if strings.TrimSpace(sch.RRule) == "" { return nil }
	start := time.Now().Truncate(24*time.Hour)
	r, err := rrule.StrToRRule(sch.RRule)
	if err != nil { log.Printf("[warn] RRULE parse failed: %v", err); return nil }
	t0 := start
	t1 := start.Add(time.Duration(cycleDays)*24*time.Hour)
	occ := r.Between(t0, t1, true)
	days := []int{}
	for _, t := range occ {
		d := int(t.Sub(t0).Hours()/24.0 + 0.00001)
		if d >= 0 && d < cycleDays { days = append(days, d) }
	}
	return days
}

// --- Heatmap aggregation ---
type HeatCell struct {
	H3Index string  `json:"h3"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Value   float64 `json:"value"`
}

func (s *server) heatmapFor(sc Scenario, feature string, day int, h3Level int) []HeatCell {
	cycle := sc.Params.CycleDays; if cycle==0 { cycle = s.cfg.Embedding.CycleDays }
	m := map[string]float64{}
	for _, acc := range sc.Accounts {
		active := expandSchedule(acc.Schedule, cycle)
		if len(active)==0 {
			if more := expandScheduleRRULE(acc.Schedule, cycle); len(more)>0 { active = more }
		}
		ok := false; for _, d := range active { if d==day { ok = true; break } }
		if !ok { continue }
		amp := 0.0
		switch feature {
		case "service_stop_time": amp = clamp(acc.EstimatedServiceMinutes/200.0, 0, 1)
		case "service_window_start": amp = clamp(acc.ServiceWindowStartMin/1440.0, 0, 1)
		case "service_window_duration": amp = clamp(acc.ServiceWindowDurationMin/1440.0, 0, 1)
		case "pinned_accounts":
			if strings.TrimSpace(acc.PinnedAgentID) != "" { amp = 1.0 }
		case "agents_available":
			avail := acc.AgentsAvailableRatio
			if avail <= 0 {
				maxAgents := sc.Globals.MaxAgents; if maxAgents <= 0 { maxAgents = max(1,len(sc.Agents)) }
				avail = float64(len(sc.Agents))/float64(maxAgents)
			}
			amp = clamp(avail, 0, 1)
		default:
			continue
		}
		h := h3.LatLngToCell(h3.LatLng{Lat: acc.Lat, Lng: acc.Lng}, h3Level).String()
		m[h] += amp
	}
	out := []HeatCell{}
	for hex, v := range m {
		var cell h3.Cell
		if err := cell.UnmarshalText([]byte(hex)); err != nil {
			continue
		}
		geo := h3.CellToLatLng(cell)
		out = append(out, HeatCell{H3Index: hex, Lat: geo.Lat, Lng: geo.Lng, Value: v})
	}
	return out
}

// --- VROOM stub ---
type VroomInput struct {
	Jobs     []VroomJob     `json:"jobs"`
	Vehicles []VroomVehicle `json:"vehicles"`
}
type VroomJob struct {
	Id         int        `json:"id"`
	Service    int        `json:"service"`
	Location   []float64  `json:"location"`
	TimeWindows [][]int   `json:"time_windows,omitempty"`
}
type VroomVehicle struct {
	Id    int       `json:"id"`
	Start []float64 `json:"start"`
	End   []float64 `json:"end"`
}

type SimStats struct {
	DrivingSec []float64 `json:"driving_sec_per_rep"`
	ServiceSec []float64 `json:"service_sec_per_rep"`
	RepsUsed   []int     `json:"reps_used_per_day"`
	Unassigned int       `json:"unassigned_stops"`
	TotalTravelSec  float64 `json:"total_travel_sec"`
	TotalServiceSec float64 `json:"total_service_sec"`
	TotalIdleSec    float64 `json:"total_idle_sec"`
}

func (s *server) buildVroom(sc Scenario, day int) VroomInput {
	in := VroomInput{}
	for i, ag := range sc.Agents {
		in.Vehicles = append(in.Vehicles, VroomVehicle{
			Id: i+1, Start: []float64{ag.Lng, ag.Lat}, End: []float64{ag.Lng, ag.Lat},
		})
	}
	cycle := sc.Params.CycleDays; if cycle==0 { cycle = 28 }
	active := []Account{}
	for _, acc := range sc.Accounts {
		days := expandSchedule(acc.Schedule, cycle)
		if len(days)==0 {
			if more := expandScheduleRRULE(acc.Schedule, cycle); len(more)>0 { days = more }
		}
		for _, d := range days { if d==day { active = append(active, acc); break } }
	}
	for i, acc := range active {
		job := VroomJob{
			Id: i+1,
			Service: int(clamp(acc.EstimatedServiceMinutes/200.0,0,1)*200)*60,
			Location: []float64{acc.Lng, acc.Lat},
		}
		start := int(acc.ServiceWindowStartMin)*60
		end := int(acc.ServiceWindowStartMin+acc.ServiceWindowDurationMin)*60
		if end > start { job.TimeWindows = [][]int{{start,end}} }
		in.Jobs = append(in.Jobs, job)
	}
	return in
}

func (s *server) maybeRunVroom(in VroomInput) (map[string]any, error) {
	bin := os.Getenv("VROOM_BIN")
	if bin == "" { return nil, errors.New("VROOM_BIN not set") }
	tmp := os.TempDir()
	inPath := filepath.Join(tmp, fmt.Sprintf("vroom_in_%d.json", time.Now().UnixNano()))
	outPath := filepath.Join(tmp, fmt.Sprintf("vroom_out_%d.json", time.Now().UnixNano()))
	data, _ := json.Marshal(in)
	_ = os.WriteFile(inPath, data, 0644)
	cmd := exec.Command(bin, "-i", inPath, "-o", outPath)
	var stderr bytes.Buffer; cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil { return nil, fmt.Errorf("vroom failed: %v (%s)", err, stderr.String()) }
	outB, err := os.ReadFile(outPath); if err != nil { return nil, err }
	var out map[string]any
	if err := json.Unmarshal(outB, &out); err != nil { return nil, err }
	return out, nil
}

func (s *server) naiveSim(sc Scenario, day int) SimStats {
	speedKmh := 50.0
	toSec := func(km float64) float64 { return km/speedKmh*3600.0 }
	type veh struct{ lat,lng,drive,service float64 }
	vs := make([]veh, len(sc.Agents))
	for i, ag := range sc.Agents { vs[i] = veh{lat:ag.Lat, lng:ag.Lng} }
	cycle := sc.Params.CycleDays; if cycle==0 { cycle = 28 }
	accs := []Account{}
	for _, a := range sc.Accounts {
		ds := expandSchedule(a.Schedule, cycle)
		if len(ds)==0 { ds = expandScheduleRRULE(a.Schedule, cycle) }
		for _, d := range ds { if d==day { accs = append(accs, a); break } }
	}
	assigned := 0
	for _, acc := range accs {
		// nearest vehicle
		best, bestD := 0, 1e18
		for i := range vs {
			d := havKM(vs[i].lat, vs[i].lng, acc.Lat, acc.Lng)
			if d < bestD { bestD = d; best = i }
		}
		vs[best].drive += toSec(bestD)
		vs[best].lat, vs[best].lng = acc.Lat, acc.Lng
		vs[best].service += clamp(acc.EstimatedServiceMinutes/200.0,0,1)*200*60
		assigned++
	}
	drives, services := []float64{}, []float64{}
	var totalDrive, totalService float64
	for _, v := range vs { drives = append(drives, v.drive); services = append(services, v.service); totalDrive += v.drive; totalService += v.service }
	return SimStats{
		DrivingSec: drives,
		ServiceSec: services,
		RepsUsed:   []int{len(sc.Agents)},
		Unassigned: max(0, len(sc.Accounts)-assigned),
		TotalTravelSec: totalDrive,
		TotalServiceSec: totalService,
		TotalIdleSec: 0,
	}
}

func (s *server) simToVector(stats SimStats) []float64 {
	out := []float64{}
	push := func(vals []float64){
		if len(vals)==0 { out = append(out, make([]float64,6)...); return }
		cp := append([]float64(nil), vals...); sort.Float64s(cp)
		n := float64(len(cp)); sum := 0.0; for _, v := range cp { sum += v }
		avg := sum/n; minv := cp[0]; maxv := cp[len(cp)-1]
		p := func(q float64) float64 {
			if len(cp)==1 { return cp[0] }
			pos := q*(n-1); i := int(pos); if i >= len(cp)-1 { return cp[len(cp)-1] }
			f := pos - float64(i); return cp[i]*(1-f) + cp[i+1]*f
		}
		out = append(out, minv, maxv, avg, p(0.5), p(0.75), p(0.95))
	}
	push(stats.DrivingSec)
	push(stats.ServiceSec)
	reps := []float64{}; for _, x := range stats.RepsUsed { reps = append(reps, float64(x)) }; push(reps)
	push([]float64{float64(stats.Unassigned)})
	out = append(out, stats.TotalTravelSec, stats.TotalServiceSec, stats.TotalIdleSec, float64(stats.Unassigned))
	return out
}

func havKM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2-lat1)*math.Pi/180.0
	dLon := (lon2-lon1)*math.Pi/180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2*R*math.Asin(math.Sqrt(a))
}

// --- HTTP endpoints ---
func (s *server) handleScenarioSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w,"method not allowed",405); return }
	var sc Scenario
	if err := json.NewDecoder(r.Body).Decode(&sc); err != nil { http.Error(w,"invalid json",400); return }
	id, err := s.saveScenario(sc)
	if err != nil { http.Error(w, err.Error(), 500); return }
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *server) handleIndexEmbedding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w,"method not allowed",405); return }
	var req struct{ ScenarioID string `json:"scenario_id"`; Vector []float64 `json:"vector"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w,"invalid json",400); return }
	if len(req.Vector)==0 { http.Error(w,"empty vector",400); return }
	if err := s.ensureVecTable(len(req.Vector)); err != nil { log.Printf("ensure vec: %v", err) }
	embID, err := s.saveEmbedding(req.ScenarioID, req.Vector)
	if err != nil { http.Error(w, err.Error(), 500); return }
	_ = s.indexEmbeddingVec(len(req.Vector), req.Vector)
	writeJSON(w, 200, map[string]string{"embedding_id": embID})
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w,"method not allowed",405); return }
	var req struct{ Vector []float64 `json:"vector"`; K int `json:"k"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w,"invalid json",400); return }
	hits, err := s.searchSimilar(len(req.Vector), req.Vector, req.K)
	if err != nil { http.Error(w, err.Error(), 500); return }
	writeJSON(w, 200, map[string]any{"hits": hits})
}

func (s *server) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w,"method not allowed",405); return }
	var req struct{ Scenario Scenario `json:"scenario"`; Feature string `json:"feature"`; Day int `json:"day"`; H3Level int `json:"h3_level"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w,"invalid json",400); return }
	cells := s.heatmapFor(req.Scenario, req.Feature, req.Day, req.H3Level)
	writeJSON(w, 200, map[string]any{"cells": cells})
}

func (s *server) handleSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w,"method not allowed",405); return }
	var req struct{ Scenario Scenario `json:"scenario"`; Day int `json:"day"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w,"invalid json",400); return }
	in := s.buildVroom(req.Scenario, req.Day)
	vroomOut, _ := s.maybeRunVroom(in)
	stats := s.naiveSim(req.Scenario, req.Day)
	vec := s.simToVector(stats)
	writeJSON(w, 200, map[string]any{"vroom_input": in, "vroom_output": vroomOut, "stats": stats, "vector": vec})
}

// --- helpers ---
func uuidNew() string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", rand.Uint32(), rand.Uint32()&0xffff, rand.Uint32()&0xffff, rand.Uint32()&0xffff, rand.Uint32())
}
