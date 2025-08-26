package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"

	yaml "gopkg.in/yaml.v3"
)

//go:embed web/*
var webFS embed.FS

type ServerConfig struct {
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`
	Embedding struct {
		CycleDays int   `yaml:"cycle_days"`
		H3Levels  []int `yaml:"h3_levels"`
		Resolutions struct {
			ServiceStopTime       int `yaml:"service_stop_time"`
			ServiceWindowStart    int `yaml:"service_window_start"`
			ServiceWindowDuration int `yaml:"service_window_duration"`
			PinnedAccounts        int `yaml:"pinned_accounts"`
			AgentsAvailable       int `yaml:"agents_available"`
			AgentStartLocations   int `yaml:"agent_start_locations"`
		} `yaml:"resolutions"`
		Overshoot     float64 `yaml:"overshoot"`
		BaseFrequency float64 `yaml:"base_frequency"`
	} `yaml:"embedding"`
	Storage struct {
		DBPath        string `yaml:"db_path"`
		SqliteVecPath string `yaml:"sqlite_vec_path"`
	} `yaml:"storage"`
	Map struct {
		MapboxToken string `yaml:"mapbox_token"`
		InitialViewState struct {
			Latitude  float64 `yaml:"latitude"`
			Longitude float64 `yaml:"longitude"`
			Zoom      float64 `yaml:"zoom"`
		} `yaml:"initial_view_state"`
	} `yaml:"map"`
	UI struct {
		EnableEmbeddingTab bool `yaml:"enable_embedding_tab"`
		EnableSpectralTab  bool `yaml:"enable_spectral_tab"`
		EnableHeatmapTab   bool `yaml:"enable_heatmap_tab"`
	} `yaml:"ui"`
}

type Scenario struct {
	Name     string          `json:"name"`
	Agents   []Agent         `json:"agents"`
	Accounts []Account       `json:"accounts"`
	Globals  Globals         `json:"globals"`
	Params   EmbeddingParams `json:"params"`
}

type Agent struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Lat      float64  `json:"lat"`
	Lng      float64  `json:"lng"`
	Schedule Schedule `json:"schedule"`
}

type Account struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	Lat                     float64  `json:"lat"`
	Lng                     float64  `json:"lng"`
	EstimatedServiceMinutes float64  `json:"estimated_service_minutes"`
	ServiceWindowStartMin   float64  `json:"service_window_start_min"`
	ServiceWindowDurationMin float64 `json:"service_window_duration_min"`
	PinnedAgentID           string   `json:"pinned_agent_id"`
	AgentsAvailableRatio    float64  `json:"agents_available_ratio"`
	Schedule                Schedule  `json:"schedule"`
}

type Schedule struct {
	Type   string `json:"type"`
	Anchor string `json:"anchor"`
	RRule  string `json:"rrule"`
}

type Globals struct {
	MaxAgents              int     `json:"max_agents"`
	MaxWorkMinutesPerWeek  float64 `json:"max_work_minutes_per_week"`
	MaxWorkMinutesPerDay   float64 `json:"max_work_minutes_per_day"`
	MaxTravelMinutesPerDay float64 `json:"max_travel_minutes_per_day"`
}

type EmbeddingParams struct {
	ResServiceStopTime       int   `json:"res_service_stop_time"`
	ResServiceWindowStart    int   `json:"res_service_window_start"`
	ResServiceWindowDuration int   `json:"res_service_window_duration"`
	ResPinnedAccounts        int   `json:"res_pinned_accounts"`
	ResAgentsAvailable       int   `json:"res_agents_available"`
	ResAgentStartLocations   int   `json:"res_agent_start_locations"`
	H3Levels                 []int `json:"h3_levels"`
	CycleDays                int   `json:"cycle_days"`
}

type EmbeddingResult struct {
	Embedding  []float64            `json:"embedding"`
	Components map[string][]float64 `json:"components"`
	Offsets    map[string][2]int    `json:"offsets"`
	Meta       map[string]any       `json:"meta"`
}

type server struct {
	cfg       ServerConfig
	mu        sync.RWMutex
	scenarios map[string]Scenario
	db        *sql.DB
}

func main() {
	cfgPath := "config.yaml"
	if env := os.Getenv("SPE_CONFIG"); env != "" {
		cfgPath = env
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var cfg ServerConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}

	s := &server{cfg: cfg, scenarios: map[string]Scenario{}}
	if err := s.initDB(); err != nil { log.Printf("[warn] db init: %v", err) }

	mux := http.NewServeMux()
	
	// Serve web files from the embedded filesystem
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to create sub filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webContent)))
	mux.HandleFunc("/api/config", s.handleGetConfig)
	mux.HandleFunc("/api/embed", s.handleEmbed)
	mux.HandleFunc("/api/scenarios", s.handleScenarios)
	// Extended endpoints
	mux.HandleFunc("/api/scenario/save", s.handleScenarioSave)
	mux.HandleFunc("/api/index", s.handleIndexEmbedding)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/heatmap", s.handleHeatmap)
	mux.HandleFunc("/api/simulate", s.handleSimulate)

	addr := cfg.Server.Addr
	if addr == "" { addr = ":8080" }
	log.Printf("SPE server on %s", addr)
	log.Fatal(http.ListenAndServe(addr, cors(mux)))
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"mapboxToken": s.cfg.Map.MapboxToken,
		"initialViewState": map[string]float64{
			"latitude":  s.cfg.Map.InitialViewState.Latitude,
			"longitude": s.cfg.Map.InitialViewState.Longitude,
			"zoom":      s.cfg.Map.InitialViewState.Zoom,
		},
		"ui": map[string]bool{
			"embeddingTab": s.cfg.UI.EnableEmbeddingTab,
			"spectralTab":  s.cfg.UI.EnableSpectralTab,
			"heatmapTab":   s.cfg.UI.EnableHeatmapTab,
		},
		"defaults": map[string]int{
			"resServiceStopTime":       s.cfg.Embedding.Resolutions.ServiceStopTime,
			"resServiceWindowStart":    s.cfg.Embedding.Resolutions.ServiceWindowStart,
			"resServiceWindowDuration": s.cfg.Embedding.Resolutions.ServiceWindowDuration,
			"resPinnedAccounts":        s.cfg.Embedding.Resolutions.PinnedAccounts,
			"resAgentsAvailable":       s.cfg.Embedding.Resolutions.AgentsAvailable,
			"resAgentStartLocations":   s.cfg.Embedding.Resolutions.AgentStartLocations,
		},
		"h3Levels":    s.cfg.Embedding.H3Levels,
		"cycleDays":   s.cfg.Embedding.CycleDays,
		"baseFrequency": s.cfg.Embedding.BaseFrequency,
	}
	writeJSON(w, 200, resp)
}

func (s *server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out := []Scenario{}
		for _, sc := range s.scenarios { out = append(out, sc) }
		writeJSON(w, 200, out)
	case http.MethodPost:
		var sc Scenario
		if err := json.NewDecoder(r.Body).Decode(&sc); err != nil {
			http.Error(w, "invalid json", 400); return
		}
		if sc.Name == "" { sc.Name = fmt.Sprintf("scenario-%d", len(s.scenarios)+1) }
		s.mu.Lock(); s.scenarios[sc.Name] = sc; s.mu.Unlock()
		writeJSON(w, 201, map[string]string{"id": sc.Name})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w, "method not allowed", 405); return }
	var sc Scenario
	if err := json.NewDecoder(r.Body).Decode(&sc); err != nil {
		http.Error(w, "invalid json", 400); return
	}
	res := s.buildEmbedding(sc)
	writeJSON(w, 200, res)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w); enc.SetIndent("", "  "); _ = enc.Encode(v)
}

// === Embedding core ===

func (s *server) buildEmbedding(sc Scenario) EmbeddingResult {
	p := sc.Params
	if p.ResServiceStopTime == 0 { p.ResServiceStopTime = s.cfg.Embedding.Resolutions.ServiceStopTime }
	if p.ResServiceWindowStart == 0 { p.ResServiceWindowStart = s.cfg.Embedding.Resolutions.ServiceWindowStart }
	if p.ResServiceWindowDuration == 0 { p.ResServiceWindowDuration = s.cfg.Embedding.Resolutions.ServiceWindowDuration }
	if p.ResPinnedAccounts == 0 { p.ResPinnedAccounts = s.cfg.Embedding.Resolutions.PinnedAccounts }
	if p.ResAgentsAvailable == 0 { p.ResAgentsAvailable = s.cfg.Embedding.Resolutions.AgentsAvailable }
	if p.ResAgentStartLocations == 0 { p.ResAgentStartLocations = s.cfg.Embedding.Resolutions.AgentStartLocations }
	if len(p.H3Levels) == 0 { p.H3Levels = s.cfg.Embedding.H3Levels }
	if p.CycleDays == 0 { p.CycleDays = s.cfg.Embedding.CycleDays }
	overshoot := s.cfg.Embedding.Overshoot
	baseFreq := s.cfg.Embedding.BaseFrequency

	agentVec := make([]float64, p.ResAgentStartLocations)
	for _, ag := range sc.Agents {
		latN := normalize(ag.Lat, -90, 90)
		lngN := normalize(ag.Lng, -180, 180)
		phi := phShift(latN, lngN, 0, p.CycleDays)
		superimpose(agentVec, amplitude(1.0, overshoot), baseFreq, p.H3Levels, phi)
	}
	l2Normalize(agentVec)

	pinnedVec := make([]float64, p.ResPinnedAccounts)
	agentsAvailVec := make([]float64, p.ResAgentsAvailable)
	stopTimeVec := make([]float64, p.ResServiceStopTime)
	winStartVec := make([]float64, p.ResServiceWindowStart)
	winDurVec := make([]float64, p.ResServiceWindowDuration)

	maxAgents := sc.Globals.MaxAgents
	if maxAgents <= 0 { maxAgents = max(1, len(sc.Agents)) }

	for _, acc := range sc.Accounts {
		latN := normalize(acc.Lat, -90, 90)
		lngN := normalize(acc.Lng, -180, 180)
		activeDays := expandSchedule(acc.Schedule, p.CycleDays)
		if len(activeDays) == 0 {
			// optional RRULE expansion (implemented in extended.go)
			if more := expandScheduleRRULE(acc.Schedule, p.CycleDays); len(more) > 0 {
				activeDays = more
			}
		}

		stAmp := clamp(acc.EstimatedServiceMinutes/200.0, 0, 1)
		swStartAmp := clamp(acc.ServiceWindowStartMin/1440.0, 0, 1)
		swDurAmp := clamp(acc.ServiceWindowDurationMin/1440.0, 0, 1)
		pinnedAmp := 0.0; if strings.TrimSpace(acc.PinnedAgentID) != "" { pinnedAmp = 1.0 }
		avail := acc.AgentsAvailableRatio; if avail <= 0 {
			avail = float64(len(sc.Agents)) / float64(maxAgents)
		}
		avail = clamp(avail, 0, 1)

		for _, day := range activeDays {
			phi := phShift(latN, lngN, day, p.CycleDays)
			superimpose(stopTimeVec, amplitude(stAmp, overshoot), baseFreq, p.H3Levels, phi)
			superimpose(winStartVec, amplitude(swStartAmp, overshoot), baseFreq, p.H3Levels, phi)
			superimpose(winDurVec, amplitude(swDurAmp, overshoot), baseFreq, p.H3Levels, phi)
			superimpose(pinnedVec, amplitude(pinnedAmp, overshoot), baseFreq, p.H3Levels, phi)
			superimpose(agentsAvailVec, amplitude(avail, overshoot), baseFreq, p.H3Levels, phi)
		}
	}

	l2Normalize(stopTimeVec); l2Normalize(winStartVec); l2Normalize(winDurVec)
	l2Normalize(pinnedVec); l2Normalize(agentsAvailVec)

	offsets := map[string][2]int{}
	embedding := make([]float64, 0, len(stopTimeVec)+len(winStartVec)+len(winDurVec)+len(pinnedVec)+len(agentsAvailVec)+len(agentVec))
	appendWithOffset := func(name string, v []float64) {
		start := len(embedding); embedding = append(embedding, v...); end := len(embedding); offsets[name] = [2]int{start, end}
	}
	appendWithOffset("service_stop_time", stopTimeVec)
	appendWithOffset("service_window_start", winStartVec)
	appendWithOffset("service_window_duration", winDurVec)
	appendWithOffset("pinned_accounts", pinnedVec)
	appendWithOffset("agents_available", agentsAvailVec)
	appendWithOffset("agent_start_locations", agentVec)

	components := map[string][]float64{
		"service_stop_time":       stopTimeVec,
		"service_window_start":    winStartVec,
		"service_window_duration": winDurVec,
		"pinned_accounts":         pinnedVec,
		"agents_available":        agentsAvailVec,
		"agent_start_locations":   agentVec,
	}

	meta := map[string]any{
		"h3_levels":  p.H3Levels,
		"cycle_days": p.CycleDays,
		"order": []string{"service_stop_time","service_window_start","service_window_duration","pinned_accounts","agents_available","agent_start_locations"},
	}

	return EmbeddingResult{Embedding: embedding, Components: components, Offsets: offsets, Meta: meta}
}

func expandSchedule(sch Schedule, cycleDays int) []int {
	days := []int{}
	anchorIdx := weekdayIndex(sch.Anchor)
	if sch.Type == "" && sch.RRule != "" {
		// leave to RRULE expansion
		return days
	}
	switch strings.ToUpper(sch.Type) {
	case "WEEKLY":
		for d := 0; d < cycleDays; d++ { if d%7 == anchorIdx { days = append(days, d) } }
	case "BIWEEKLY_AC":
		for d := 0; d < cycleDays; d++ { w := d/7; if (w==0||w==2) && d%7==anchorIdx { days = append(days, d) } }
	case "BIWEEKLY_BD":
		for d := 0; d < cycleDays; d++ { w := d/7; if (w==1||w==3) && d%7==anchorIdx { days = append(days, d) } }
	default:
		if strings.HasPrefix(strings.ToUpper(sch.Type), "MONTHLY_") {
			n := 0; fmt.Sscanf(sch.Type, "MONTHLY_%d", &n)
			week := clampInt(n-1, 0, 3)
			for d := week*7; d < (week+1)*7 && d < cycleDays; d++ {
				if d%7 == anchorIdx { days = append(days, d) }
			}
		}
	}
	return days
}

func weekdayIndex(anchor string) int {
	switch strings.ToUpper(strings.TrimSpace(anchor)) {
	case "MON": return 0
	case "TUE": return 1
	case "WED": return 2
	case "THU": return 3
	case "FRI": return 4
	case "SAT": return 5
	case "SUN": return 6
	default: return 0
	}
}

func normalize(x, min, max float64) float64 {
	if max <= min { return 0 }
	t := (x - min) / (max - min)
	if t < 0 { t = 0 } else if t > 1 { t = 1 }
	return t
}
func clamp(x, a, b float64) float64 { if x<a {return a}; if x>b {return b}; return x }
func clampInt(x, a, b int) int { if x<a {return a}; if x>b {return b}; return x }
func phShift(latN, lngN float64, day, cycle int) float64 {
	phiSpace := (latN-0.5)*math.Pi + (lngN-0.5)*math.Pi
	phiDay := 2*math.Pi*float64(day)/float64(max(1, cycle))
	return phiSpace + phiDay
}
func frequencyForLevel(base float64, level int) float64 {
	shift := float64(level-5)
	scale := math.Pow(2, shift)
	return base / scale
}
func amplitude(norm, overshoot float64) float64 { return clamp(norm*(1+overshoot), 0, 1+overshoot) }
func superimpose(vec []float64, amp, baseFreq float64, h3Levels []int, phi float64) {
	n := float64(len(vec)); if n==0 || amp==0 { return }
	for _, lvl := range h3Levels {
		f := frequencyForLevel(baseFreq, lvl)
		for i := range vec {
			theta := 2*math.Pi*f*(float64(i)/n) + phi
			vec[i] += amp * math.Sin(theta)
		}
	}
}
func l2Normalize(v []float64) {
	var ss float64; for _, x := range v { ss += x*x }
	if ss == 0 { return }
	inv := 1.0/math.Sqrt(ss); for i := range v { v[i] *= inv }
}
func max(a,b int) int { if a>b {return a}; return b }
