/* global React, ReactDOM, deck, mapboxgl, d3 */

// Wait for all libraries to load
if (typeof deck === 'undefined' || typeof mapboxgl === 'undefined' || typeof d3 === 'undefined') {
  console.error('Required libraries not loaded. Waiting...');
  window.addEventListener('load', () => {
    console.log('Retrying after load event...');
    // The script will be re-evaluated by Babel
  });
}

const {useState, useEffect, useRef} = React;
const {MapboxOverlay, ScatterplotLayer} = (typeof deck !== 'undefined' ? deck : {});

function TopBar({mode, setMode, dayFilters, setDayFilters, policyFilters, setPolicyFilters, onEmbed, onSimulate, onHeatmap}) {
  const toggleMode = (m) => () => setMode(m);
  const toggleDay = (d) => () => setDayFilters(prev => ({...prev, [d]: !prev[d]}));
  const togglePolicy = (p) => () => setPolicyFilters(prev => ({...prev, [p]: !prev[p]}));
  return (
    <div className="topbar">
      <div>
        <button className={"btn " + (mode==='select'?'primary':'')} onClick={toggleMode('select')}>Select</button>
        <button className={"btn " + (mode==='account'?'primary':'')} onClick={toggleMode('account')}>Create Account</button>
        <button className={"btn " + (mode==='agent'?'primary':'')} onClick={toggleMode('agent')}>Create Agent</button>
        <button className={"btn " + (mode==='erase'?'primary':'')} onClick={toggleMode('erase')}>Delete</button>
      </div>
      <div>
        {['Mon','Tue','Wed','Thu','Fri'].map((d,i)=>(
          <label key={d} className="toggle"><input type="checkbox" checked={dayFilters[i]||false} onChange={toggleDay(i)} /> {d}</label>
        ))}
        {['Weekly','BiWeekly-AC','BiWeekly-BD','Monthly-1','Monthly-2','Monthly-3','Monthly-4'].map(p => (
          <label key={p} className="toggle"><input type="checkbox" checked={policyFilters[p]||false} onChange={togglePolicy(p)} /> {p}</label>
        ))}
        <button className="btn primary" onClick={onEmbed}>Build Embedding</button>
        <button className="btn" onClick={onSimulate}>Simulate (VROOM)</button>
        <button className="btn" onClick={onHeatmap}>Update Heatmap</button>
      </div>
    </div>
  );
}

function SidePanel({agents, accounts, selectAgent, selectAccount, selected, updateAgent, updateAccount, activeTab, setActiveTab, lastEmbedding, res, setRes, onSaveScenario, onIndexEmbedding, onSearch, searchK, setSearchK, searchHits, heat, setHeat}) {
  const [selectedComponent, setSelectedComponent] = React.useState('service_stop_time');
  const tabs = [["entities","Entities"],["embedding","Embedding"],["spectral","Spectral"],["heatmap","Heatmap"]];
  return (
    <div className="sidepanel">
      <div className="tabbar">{tabs.map(([k,label]) => <div key={k} className={"tab "+(activeTab===k?'active':'')} onClick={()=>setActiveTab(k)}>{label}</div>)}</div>

      {activeTab==='entities' && (
        <div>
          <div className="section">
            <h3>Agents</h3>
            <ul className="entity-list">
              {agents.map(a => (
                <li key={a.id} className="entity-item" onClick={()=>selectAgent(a.id)}>
                  <div className="title">{a.name}</div>
                  <small>({a.lat.toFixed(4)}, {a.lng.toFixed(4)}) • {a.schedule.type || 'WEEKLY'} {a.schedule.anchor || 'MON'}</small>
                  {selected?.type==='agent' && selected?.id===a.id && (
                    <div className="entity-actions">
                      <input className="input" value={a.name} onChange={e=>updateAgent(a.id, {name:e.target.value})} placeholder="Name"/>
                      <select value={a.schedule.type} onChange={e=>updateAgent(a.id, {schedule:{...a.schedule, type:e.target.value}})}>
                        <option>WEEKLY</option><option>BIWEEKLY_AC</option><option>BIWEEKLY_BD</option>
                        <option>MONTHLY_1</option><option>MONTHLY_2</option><option>MONTHLY_3</option><option>MONTHLY_4</option>
                      </select>
                      <select value={a.schedule.anchor} onChange={e=>updateAgent(a.id, {schedule:{...a.schedule, anchor:e.target.value}})}>
                        {['MON','TUE','WED','THU','FRI'].map(d => <option key={d}>{d}</option>)}
                      </select>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </div>
          <div className="section">
            <h3>Accounts</h3>
            <ul className="entity-list">
              {accounts.map(ac => (
                <li key={ac.id} className="entity-item" onClick={()=>selectAccount(ac.id)}>
                  <div className="title">{ac.name}</div>
                  <small>({ac.lat.toFixed(4)}, {ac.lng.toFixed(4)}) • {ac.schedule.type || 'WEEKLY'} {ac.schedule.anchor || 'MON'}</small>
                  {selected?.type==='account' && selected?.id===ac.id && (
                    <div className="entity-actions" style={{flexDirection:'column', alignItems:'stretch'}}>
                      <input className="input" value={ac.name} onChange={e=>updateAccount(ac.id, {name:e.target.value})} placeholder="Name"/>
                      <div className="row"><span>Service (min)</span><input className="input" type="number" min="0" max="200" value={ac.estimated_service_minutes} onChange={e=>updateAccount(ac.id, {estimated_service_minutes:+e.target.value})}/></div>
                      <div className="row"><span>Window Start (min)</span><input className="input" type="number" min="0" max="1440" value={ac.service_window_start_min} onChange={e=>updateAccount(ac.id, {service_window_start_min:+e.target.value})}/></div>
                      <div className="row"><span>Window Duration (min)</span><input className="input" type="number" min="0" max="1440" value={ac.service_window_duration_min} onChange={e=>updateAccount(ac.id, {service_window_duration_min:+e.target.value})}/></div>
                      <div className="row"><span>Pinned Agent</span>
                        <select value={ac.pinned_agent_id} onChange={e=>updateAccount(ac.id, {pinned_agent_id:e.target.value})}>
                          <option value="">(none)</option>{agents.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
                        </select>
                      </div>
                      <div className="row"><span>Schedule</span>
                        <select value={ac.schedule.type} onChange={e=>updateAccount(ac.id, {schedule:{...ac.schedule, type:e.target.value}})}>
                          <option>WEEKLY</option><option>BIWEEKLY_AC</option><option>BIWEEKLY_BD</option>
                          <option>MONTHLY_1</option><option>MONTHLY_2</option><option>MONTHLY_3</option><option>MONTHLY_4</option>
                        </select>
                        <select value={ac.schedule.anchor} onChange={e=>updateAccount(ac.id, {schedule:{...ac.schedule, anchor:e.target.value}})}>
                          {['MON','TUE','WED','THU','FRI'].map(d => <option key={d}>{d}</option>)}
                        </select>
                      </div>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}

      {activeTab==='embedding' && (
        <div className="section">
          <h3>Last Embedding</h3>
          {!lastEmbedding && <div className="legend">Click <span className="badge">Build Embedding</span> to generate.</div>}
          {lastEmbedding && (
            <div>
              <div className="kv">
                <span>Dim</span><b>{lastEmbedding.embedding.length}</b>
                <span>Components</span><b>{Object.keys(lastEmbedding.components).length}</b>
              </div>
              <div style={{marginTop:8}} className="legend">Order: {lastEmbedding.meta?.order?.join(" → ") || 'N/A'}</div>
              <div style={{marginTop:12}}>
                <h4 style={{fontSize: '13px', color: 'var(--accent)', marginBottom: 8}}>Embedding Vector (first 512 values)</h4>
                <LineChart data={lastEmbedding.embedding.slice(0,512)} />
              </div>
            </div>
          )}
          <div className="section">
            <h3>Resolutions</h3>
            <div className="row"><span>Service Stop Time</span><input className="input" type="number" min="32" step="32" value={res.res_service_stop_time} onChange={e=>setRes({...res, res_service_stop_time:+e.target.value})}/></div>
            <div className="row"><span>Service Window Start</span><input className="input" type="number" min="32" step="32" value={res.res_service_window_start} onChange={e=>setRes({...res, res_service_window_start:+e.target.value})}/></div>
            <div className="row"><span>Service Window Duration</span><input className="input" type="number" min="32" step="32" value={res.res_service_window_duration} onChange={e=>setRes({...res, res_service_window_duration:+e.target.value})}/></div>
            <div className="row"><span>Pinned Accounts</span><input className="input" type="number" min="16" step="16" value={res.res_pinned_accounts} onChange={e=>setRes({...res, res_pinned_accounts:+e.target.value})}/></div>
            <div className="row"><span>Agents Available</span><input className="input" type="number" min="16" step="16" value={res.res_agents_available} onChange={e=>setRes({...res, res_agents_available:+e.target.value})}/></div>
            <div className="row"><span>Agent Start Locations</span><input className="input" type="number" min="16" step="16" value={res.res_agent_start_locations} onChange={e=>setRes({...res, res_agent_start_locations:+e.target.value})}/></div>
            <div className="row"><button className="btn" onClick={onSaveScenario}>Save Scenario</button></div>
            {lastEmbedding && <div className="row"><button className="btn" onClick={onIndexEmbedding}>Index Embedding</button></div>}
            {lastEmbedding && <div className="row" style={{gap:8}}>
              <input className="input" type="number" min="1" value={searchK} onChange={e=>setSearchK(+e.target.value)} />
              <button className="btn" onClick={onSearch}>Search Similar (k)</button>
            </div>}
            {searchHits && searchHits.length>0 && <div className="legend">Hits: {searchHits.map(h=>h.ref+':'+h.distance.toFixed(3)).join(', ')}</div>}
          </div>
        </div>
      )}

      {activeTab==='spectral' && (
        <div className="section">
          <h3>Spectral Analysis</h3>
          <div className="legend">FFT magnitude spectrum of the embedding components</div>
          {lastEmbedding && lastEmbedding.components ? (
            <>
              <div style={{marginTop: 10}}>
                <label className="legend">Component: </label>
                <select 
                  value={selectedComponent}
                  style={{marginLeft: 8, padding: '4px 8px', background: '#0c141e', color: '#e6f0ff', border: '1px solid #1f2a3a', borderRadius: '6px'}}
                  onChange={(e) => {
                    setSelectedComponent(e.target.value);
                    const componentData = lastEmbedding.components[e.target.value];
                    console.log('Selected component:', e.target.value, 'Length:', componentData?.length);
                  }}
                >
                  {Object.keys(lastEmbedding.components).map(key => (
                    <option key={key} value={key}>{key.replace(/_/g, ' ')}</option>
                  ))}
                </select>
              </div>
              {lastEmbedding.components[selectedComponent] && (
                <Spectrum data={lastEmbedding.components[selectedComponent]} />
              )}
            </>
          ) : (
            <div className="legend">No embedding yet. Click "Build Embedding" to generate.</div>
          )}
        </div>
      )}

      {activeTab==='heatmap' && (
        <div className="section">
          <h3>Heatmap Controls</h3>
          <div className="legend">Pick feature/day/H3 level, then click Update Heatmap.</div>
          <div className="row"><span>Feature</span>
            <select value={heat.feature} onChange={e=>setHeat({...heat, feature:e.target.value})}>
              <option value="service_stop_time">Service Stop Time</option>
              <option value="service_window_start">Service Window Start</option>
              <option value="service_window_duration">Service Window Duration</option>
              <option value="pinned_accounts">Pinned Accounts</option>
              <option value="agents_available">Agents Available</option>
            </select>
          </div>
          <div className="row"><span>Day</span><input className="input" type="number" min="0" max="27" value={heat.day} onChange={e=>setHeat({...heat, day:+e.target.value})}/></div>
          <div className="row"><span>H3 Level</span><input className="input" type="number" min="5" max="11" value={heat.h3_level} onChange={e=>setHeat({...heat, h3_level:+e.target.value})}/></div>
        </div>
      )}
    </div>
  );
}

function LineChart({data}) {
  const ref = useRef(null);
  useEffect(()=>{
    if (!data || data.length===0) {
      console.log('LineChart: No data to display');
      return;
    }
    
    const el = ref.current; 
    if (!el) {
      console.error('LineChart: No ref element');
      return;
    }
    
    el.innerHTML='';
    const width = el.clientWidth || 300;
    const height = 220;
    
    console.log('LineChart: Drawing', data.length, 'points, width:', width);
    
    const svg = d3.select(el).append('svg')
      .attr('width', width)
      .attr('height', height)
      .style('background', '#0b121b')
      .style('border', '1px solid #1a2432')
      .style('border-radius', '12px');
    
    const margin = {top: 20, right: 20, bottom: 30, left: 40};
    const innerWidth = width - margin.left - margin.right;
    const innerHeight = height - margin.top - margin.bottom;
    
    const g = svg.append('g')
      .attr('transform', `translate(${margin.left},${margin.top})`);
    
    const x = d3.scaleLinear()
      .domain([0, data.length-1])
      .range([0, innerWidth]);
    
    const extent = d3.extent(data);
    const yMin = Math.min(extent[0], -0.1);
    const yMax = Math.max(extent[1], 0.1);
    
    const y = d3.scaleLinear()
      .domain([yMin, yMax])
      .nice()
      .range([innerHeight, 0]);
    
    // Add gridlines
    g.append('g')
      .attr('class', 'grid')
      .attr('transform', `translate(0,${innerHeight})`)
      .call(d3.axisBottom(x).tickSize(-innerHeight).tickFormat(''))
      .style('stroke-dasharray', '3,3')
      .style('opacity', 0.3);
    
    g.append('g')
      .attr('class', 'grid')
      .call(d3.axisLeft(y).tickSize(-innerWidth).tickFormat(''))
      .style('stroke-dasharray', '3,3')
      .style('opacity', 0.3);
    
    // Draw the line
    const line = d3.line()
      .x((d,i)=>x(i))
      .y(d=>y(d))
      .curve(d3.curveMonotoneX);
    
    g.append('path')
      .datum(data)
      .attr('d', line)
      .attr('fill','none')
      .attr('stroke','#5dd6ff')
      .attr('stroke-width',2);
    
    // Add axes
    g.append('g')
      .attr('transform', `translate(0,${innerHeight})`)
      .call(d3.axisBottom(x).ticks(6))
      .style('color','#7c8aa0');
    
    g.append('g')
      .call(d3.axisLeft(y).ticks(5))
      .style('color','#7c8aa0');
    
    // Add labels
    g.append('text')
      .attr('transform', 'rotate(-90)')
      .attr('y', 0 - margin.left)
      .attr('x', 0 - (innerHeight / 2))
      .attr('dy', '1em')
      .style('text-anchor', 'middle')
      .style('fill', '#7c8aa0')
      .style('font-size', '12px')
      .text('Magnitude');
    
    g.append('text')
      .attr('transform', `translate(${innerWidth / 2}, ${innerHeight + margin.bottom})`)
      .style('text-anchor', 'middle')
      .style('fill', '#7c8aa0')
      .style('font-size', '12px')
      .text('Frequency Bin');
      
  }, [data]);
  
  return <div ref={ref} style={{width:'100%', height:220, marginTop: 10}}/>;
}

function Spectrum({data}) {
  const [spec, setSpec] = React.useState([]);
  React.useEffect(()=>{
    if (!data || data.length === 0) {
      console.log('Spectrum: No data provided');
      return;
    }
    console.log('Spectrum: Computing FFT for', data.length, 'samples');
    const N = data.length; 
    const magnitudes = [];
    
    // Compute FFT magnitudes
    for (let k=0; k<Math.min(256,N); k++){
      let re=0, im=0;
      for (let n=0; n<N; n++){
        const phi = -2*Math.PI*k*n/N;
        re += data[n]*Math.cos(phi); 
        im += data[n]*Math.sin(phi);
      }
      magnitudes.push(Math.sqrt(re*re+im*im));
    }
    
    // Find max for normalization
    const max = Math.max(...magnitudes);
    console.log('Spectrum: Max magnitude:', max, 'First 10 mags:', magnitudes.slice(0,10));
    
    if (max === 0) {
      console.warn('Spectrum: All magnitudes are zero');
      setSpec(new Array(magnitudes.length).fill(0));
    } else {
      const normalized = magnitudes.map(v=>v/max);
      console.log('Spectrum: First 10 normalized:', normalized.slice(0,10));
      setSpec(normalized);
    }
  }, [data]);
  
  if (!spec || spec.length === 0) {
    return <div className="legend">Computing spectrum...</div>;
  }
  
  return <LineChart data={spec} />;
}

function App(){
  const [cfg, setCfg] = useState(null);
  const [mode, setMode] = useState('select');
  const [agents, setAgents] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [selected, setSelected] = useState(null);
  const [dayFilters, setDayFilters] = useState({0:true,1:true,2:true,3:true,4:true});
  const [policyFilters, setPolicyFilters] = useState({"Weekly":true});
  const [activeTab, setActiveTab] = useState('entities');
  const [lastEmbedding, setLastEmbedding] = useState(null);

  const [res, setRes] = useState({
    res_service_stop_time: 1024,
    res_service_window_start: 1024,
    res_service_window_duration: 1024,
    res_pinned_accounts: 64,
    res_agents_available: 128,
    res_agent_start_locations: 64,
  });
  const [heat, setHeat] = useState({feature:'service_stop_time', day:0, h3_level:7});
  const [searchK, setSearchK] = useState(5);
  const [searchHits, setSearchHits] = useState(null);

  const mapRef = useRef(null);
  const deckRef = useRef(null);
  const modeRef = useRef(mode);
  const agentsRef = useRef(agents);
  const accountsRef = useRef(accounts);
  
  // Keep refs updated
  useEffect(() => {
    modeRef.current = mode;
  }, [mode]);
  
  useEffect(() => {
    agentsRef.current = agents;
  }, [agents]);
  
  useEffect(() => {
    accountsRef.current = accounts;
  }, [accounts]);

  useEffect(()=>{
    fetch('/api/config')
      .then(r => {
        if (!r.ok) throw new Error(`HTTP error! status: ${r.status}`);
        return r.json();
      })
      .then((c) => { 
        console.log('Config loaded:', c);
        setCfg(c); 
        setRes({
          res_service_stop_time: c?.defaults?.resServiceStopTime||1024,
          res_service_window_start: c?.defaults?.resServiceWindowStart||1024,
          res_service_window_duration: c?.defaults?.resServiceWindowDuration||1024,
          res_pinned_accounts: c?.defaults?.resPinnedAccounts||64,
          res_agents_available: c?.defaults?.resAgentsAvailable||128,
          res_agent_start_locations: c?.defaults?.resAgentStartLocations||64,
        }); 
      })
      .catch(err => {
        console.error('Failed to load config:', err);
        // Set default config as fallback
        const defaultConfig = {
          mapboxToken: '',
          initialViewState: { latitude: 37.8, longitude: -85.0, zoom: 6 },
          ui: { embeddingTab: true, spectralTab: true, heatmapTab: true },
          defaults: {
            resServiceStopTime: 1024,
            resServiceWindowStart: 1024,
            resServiceWindowDuration: 1024,
            resPinnedAccounts: 64,
            resAgentsAvailable: 128,
            resAgentStartLocations: 64
          },
          h3Levels: [5,6,7,8,9,10,11],
          cycleDays: 28,
          baseFrequency: 16
        };
        setCfg(defaultConfig);
        setRes({
          res_service_stop_time: 1024,
          res_service_window_start: 1024,
          res_service_window_duration: 1024,
          res_pinned_accounts: 64,
          res_agents_available: 128,
          res_agent_start_locations: 64
        });
      });
  },[]);

  useEffect(()=>{
    if (!cfg || mapRef.current) return;
    
    // Ensure we have valid coordinates before initializing the map
    const lng = cfg.initialViewState?.longitude ?? -85.0;
    const lat = cfg.initialViewState?.latitude ?? 37.8;
    const zoom = cfg.initialViewState?.zoom ?? 6;
    
    if (isNaN(lng) || isNaN(lat)) {
      console.error('Invalid coordinates in config:', cfg.initialViewState);
      return;
    }
    
    mapboxgl.accessToken = cfg.mapboxToken || '';
    const map = new mapboxgl.Map({
      container: 'map-container',
      style: 'mapbox://styles/mapbox/dark-v11',
      center: [lng, lat],
      zoom: zoom
    });
    mapRef.current = map;
    const overlay = new MapboxOverlay({interleaved:true});
    map.addControl(overlay);
    deckRef.current = overlay;
    map.on('click', (e)=>{
      const {lngLat} = e;
      const currentMode = modeRef.current;
      const currentAgents = agentsRef.current;
      const currentAccounts = accountsRef.current;
      
      if (currentMode==='agent') {
        const id = crypto.randomUUID();
        const a = {id, name:`Agent ${currentAgents.length+1}`, lat: lngLat.lat, lng: lngLat.lng, schedule:{type:'WEEKLY', anchor:'MON'}};
        setAgents(prev=>[...prev, a]);
      } else if (currentMode==='account') {
        const id = crypto.randomUUID();
        const ac = {id, name:`Account ${currentAccounts.length+1}`, lat: lngLat.lat, lng: lngLat.lng, estimated_service_minutes:60, service_window_start_min:480, service_window_duration_min:300, pinned_agent_id:"", schedule:{type:'WEEKLY', anchor:'MON'}};
        setAccounts(prev=>[...prev, ac]);
      } else if (currentMode==='erase') {
        const thresholdKm = 0.5;
        const all = currentAgents.map((a,i)=>({type:'agent', idx:i, lat:a.lat, lng:a.lng})).concat(currentAccounts.map((a,i)=>({type:'account', idx:i, lat:a.lat, lng:a.lng})));
        let best=null, bestD=1e9;
        for (const it of all){
          const d = haversine(it.lat, it.lng, lngLat.lat, lngLat.lng);
          if (d < bestD){ bestD = d; best = it; }
        }
        if (best && bestD < thresholdKm){
          if (best.type==='agent'){ setAgents(prev=>prev.filter((_,i)=>i!==best.idx)); }
          else { setAccounts(prev=>prev.filter((_,i)=>i!==best.idx)); }
        }
      }
    });
    return () => map.remove();
  }, [cfg]); // Only depend on cfg, not on mode or agents/accounts

  useEffect(()=>{
    if (!deckRef.current) return;
    const layers = [
      new ScatterplotLayer({ id:'agents', data: agents, getPosition:d=>[d.lng,d.lat], getRadius:1500, radiusUnits:'meters', pickable:true, getFillColor:[90,220,255,200]}),
      new ScatterplotLayer({ id:'accounts', data: accounts, getPosition:d=>[d.lng,d.lat], getRadius:1000, radiusUnits:'meters', pickable:true, getFillColor:[122,255,178,200]}),
    ];
    deckRef.current.setProps({layers});
  }, [agents, accounts]);

  function selectAgent(id){ setSelected({type:'agent', id}); }
  function selectAccount(id){ setSelected({type:'account', id}); }
  function updateAgent(id, patch){ setAgents(prev=>prev.map(a=>a.id===id? {...a, ...patch, schedule:{...a.schedule, ...(patch.schedule||{})}}:a)); }
  function updateAccount(id, patch){ setAccounts(prev=>prev.map(a=>a.id===id? {...a, ...patch, schedule:{...a.schedule, ...(patch.schedule||{})}}:a)); }

  function currentScenario(){
    return {
      name: "POC",
      agents, accounts,
      globals: { max_agents: Math.max(1, agents.length), max_work_minutes_per_week:2400, max_work_minutes_per_day:600, max_travel_minutes_per_day:480 },
      params: {
        res_service_stop_time: res.res_service_stop_time,
        res_service_window_start: res.res_service_window_start,
        res_service_window_duration: res.res_service_window_duration,
        res_pinned_accounts: res.res_pinned_accounts,
        res_agents_available: res.res_agents_available,
        res_agent_start_locations: res.res_agent_start_locations,
        h3_levels: cfg?.h3Levels || [5,6,7,8,9,10,11],
        cycle_days: cfg?.cycleDays || 28
      }
    };
  }

  async function onEmbed(){
    const scenario = currentScenario();
    const res = await fetch('/api/embed', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(scenario)});
    const data = await res.json(); setLastEmbedding(data); setActiveTab('embedding');
  }
  async function onSaveScenario(){
    const scenario = currentScenario();
    const r = await fetch('/api/scenario/save', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(scenario)});
    const d = await r.json(); alert('Saved scenario id: '+d.id);
  }
  async function onIndexEmbedding(){
    if (!lastEmbedding) return;
    const scenario = currentScenario();
    const r1 = await fetch('/api/scenario/save', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(scenario)});
    const d1 = await r1.json();
    const r2 = await fetch('/api/index', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({scenario_id:d1.id, vector:lastEmbedding.embedding})});
    const d2 = await r2.json();
    alert('Indexed embedding: '+d2.embedding_id);
  }
  async function onSearch(){
    if (!lastEmbedding) return;
    const r = await fetch('/api/search', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({vector:lastEmbedding.embedding, k: searchK})});
    const d = await r.json(); setSearchHits(d.hits||[]);
  }
  async function onSimulate(){
    const scenario = currentScenario();
    const r = await fetch('/api/simulate', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({scenario, day:0})});
    const d = await r.json();
    alert('Sim complete. Unassigned: '+(d?.stats?.unassigned_stops ?? 'n/a'));
  }
  async function onHeatmap(){
    const scenario = currentScenario();
    const r = await fetch('/api/heatmap', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({scenario, feature:heat.feature, day:heat.day, h3_level:heat.h3_level})});
    const d = await r.json();
    if (deckRef.current){
      const cells = d.cells||[];
      deckRef.current.setProps({layers:[
        new deck.ScatterplotLayer({ id:'agents', data: agents, getPosition:d=>[d.lng,d.lat], getRadius:1500, radiusUnits:'meters', getFillColor:[90,220,255,200]}),
        new deck.ScatterplotLayer({ id:'accounts', data: accounts, getPosition:d=>[d.lng,d.lat], getRadius:1000, radiusUnits:'meters', getFillColor:[122,255,178,200]}),
        new deck.ScatterplotLayer({ id:'heat', data: cells, getPosition:d=>[d.lng,d.lat], getRadius:d=>500+Math.min(5000,d.value*2000), radiusUnits:'meters', getFillColor:[255,160,60,160], pickable:true })
      ]});
    }
  }

  // Show loading state while config is being fetched
  if (!cfg) {
    return (
      <div style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        height: '100vh',
        background: 'var(--bg)',
        color: 'var(--text)'
      }}>
        <div style={{textAlign: 'center'}}>
          <h2>Loading SPE POC...</h2>
          <p>Initializing configuration...</p>
        </div>
      </div>
    );
  }

  return (
    <>
      <TopBar mode={mode} setMode={setMode} dayFilters={dayFilters} setDayFilters={setDayFilters} policyFilters={policyFilters} setPolicyFilters={setPolicyFilters} onEmbed={onEmbed} onSimulate={onSimulate} onHeatmap={onHeatmap} />
      <div className="main">
        <div id="map-container" className={`mode-${mode}`}></div>
        {mode !== 'select' && (
          <div style={{
            position: 'absolute',
            top: '10px',
            left: '50%',
            transform: 'translateX(-50%)',
            background: 'rgba(20, 38, 58, 0.9)',
            padding: '8px 16px',
            borderRadius: '20px',
            border: '1px solid #2c3f57',
            zIndex: 10,
            fontSize: '14px',
            color: '#5dd6ff'
          }}>
            {mode === 'agent' && '✨ Click on the map to create an agent'}
            {mode === 'account' && '📍 Click on the map to create an account'}
            {mode === 'erase' && '🗑️ Click near an entity to delete it'}
          </div>
        )}
      </div>
      <SidePanel agents={agents} accounts={accounts} selectAgent={(id)=>setSelected({type:'agent',id})} selectAccount={(id)=>setSelected({type:'account',id})}
        selected={selected} updateAgent={updateAgent} updateAccount={updateAccount} activeTab={activeTab} setActiveTab={setActiveTab}
        lastEmbedding={lastEmbedding} res={res} setRes={setRes} onSaveScenario={onSaveScenario} onIndexEmbedding={onIndexEmbedding}
        onSearch={onSearch} searchK={searchK} setSearchK={setSearchK} searchHits={searchHits} heat={heat} setHeat={setHeat} />
    </>
  );
}

function haversine(lat1, lon1, lat2, lon2){
  const R = 6371;
  const dLat = (lat2-lat1)*Math.PI/180;
  const dLon = (lon2-lon1)*Math.PI/180;
  const a = Math.sin(dLat/2)**2 + Math.cos(lat1*Math.PI/180)*Math.cos(lat2*Math.PI/180)*Math.sin(dLon/2)**2;
  return 2*R*Math.asin(Math.sqrt(a));
}

// Initialize the app when DOM is ready
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', () => {
    ReactDOM.createRoot(document.getElementById('app')).render(<App/>);
  });
} else {
  ReactDOM.createRoot(document.getElementById('app')).render(<App/>);
}
